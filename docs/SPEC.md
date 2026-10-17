# Upwatch — Spécification

> Projet vitrine Platform/DevOps : un service Go de monitoring d'URL, déployé sur Kubernetes (k3d) avec Terraform, Helm, GitHub Actions et Prometheus/Grafana.
> Objectif : un dépôt public, propre, lançable en **une commande** (`make up`), réalisable en **un week-end**.

---

## 1. Objectifs

### Ce que le projet doit démontrer
- Écrire un service backend en **Go** idiomatique, testé et observable.
- Le conteneuriser proprement (image légère, non-root).
- Provisionner l'infra en **Infrastructure as Code** (Terraform).
- Le déployer sur **Kubernetes** via un **chart Helm** maison.
- Industrialiser avec une **CI GitHub Actions**.
- L'opérer : **métriques Prometheus, dashboard Grafana, alerte**.
- Documenter ses choix comme un ingénieur plateforme (README).

### Hors périmètre (v1)
- Interface web (l'API + Grafana suffisent).
- Authentification / multi-tenant.
- Déploiement continu automatique sur le cluster (la CI s'arrête au push d'image).
- Cloud public (tout tourne en local, gratuit).
- Multi-environnements (un seul : `local`).

---

## 2. Architecture

```
                 ┌──────────────── cluster k3d "upwatch" ────────────────┐
                 │                                                        │
  curl / client ─┼─▶ Ingress (Traefik) ─▶ Service upwatch-api            │
                 │                          │                             │
                 │                 ┌────────┴────────┐                    │
                 │                 │  Deployment     │   ns: upwatch      │
                 │                 │  upwatch        │                    │
                 │                 │  (API + worker) │──▶ PostgreSQL      │
                 │                 └────────┬────────┘    (Helm Bitnami)  │
                 │                          │ /metrics                    │
                 │                          ▼                             │
                 │       Prometheus ◀── ServiceMonitor     ns: monitoring │
                 │           │                                            │
                 │           ▼                                            │
                 │        Grafana (dashboard provisionné)                 │
                 └────────────────────────────────────────────────────────┘

  Terraform (providers kubernetes + helm) : namespaces, PostgreSQL, kube-prometheus-stack, release upwatch
  GitHub Actions : lint → test → build → push ghcr.io
```

**Choix structurant :** un seul binaire qui fait API + worker (flag/env pour activer le worker). Plus simple pour un week-end ; la séparation en deux Deployments est listée en amélioration.

---

## 3. Service Go

### 3.1 Stack
| Besoin | Choix |
|---|---|
| Langage | Go (version stable récente, ≥ 1.24) |
| HTTP | `net/http` stdlib (routing Go 1.22+ : `mux.HandleFunc("GET /targets/{id}", …)`) |
| Base de données | PostgreSQL, driver `github.com/jackc/pgx/v5` (`pgxpool`) |
| Migrations | Fichiers SQL embarqués (`embed`) appliqués au démarrage, ou `golang-migrate` |
| Métriques | `github.com/prometheus/client_golang` |
| Logs | `log/slog` en JSON |
| Config | Variables d'environnement uniquement |
| Tests | `testing` stdlib + `httptest` ; tests DB via `testcontainers-go` (optionnel) |

Pas de framework web, pas d'ORM : on veut montrer du Go simple et lisible.

### 3.2 Modèle de données

```sql
CREATE TABLE targets (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL,
    url         TEXT        NOT NULL UNIQUE,
    interval_s  INTEGER     NOT NULL DEFAULT 30 CHECK (interval_s >= 10),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE checks (
    id           BIGSERIAL PRIMARY KEY,
    target_id    BIGINT      NOT NULL REFERENCES targets(id) ON DELETE CASCADE,
    checked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    status_code  INTEGER,               -- NULL si erreur réseau
    latency_ms   INTEGER,
    success      BOOLEAN     NOT NULL,
    error        TEXT
);

CREATE INDEX idx_checks_target_time ON checks (target_id, checked_at DESC);
```

Rétention : le worker supprime les checks de plus de 7 jours (une fois par heure).

### 3.3 API REST

Base : `/api/v1`. Réponses JSON. Erreurs au format `{"error": "message"}`.

| Méthode | Route | Description | Codes |
|---|---|---|---|
| `POST` | `/api/v1/targets` | Créer une cible `{name, url, interval_s?}` | 201, 400, 409 |
| `GET` | `/api/v1/targets` | Lister les cibles avec leur dernier statut | 200 |
| `GET` | `/api/v1/targets/{id}` | Détail d'une cible + 20 derniers checks | 200, 404 |
| `DELETE` | `/api/v1/targets/{id}` | Supprimer une cible | 204, 404 |
| `GET` | `/api/v1/targets/{id}/uptime?window=24h` | % de succès sur la fenêtre (`1h`, `24h`, `7d`) | 200, 400, 404 |

Endpoints techniques (hors `/api`) :

| Route | Rôle |
|---|---|
| `GET /healthz` | Liveness : le process répond (pas de check DB) → 200 |
| `GET /readyz` | Readiness : ping PostgreSQL OK → 200, sinon 503 |
| `GET /metrics` | Métriques Prometheus |

Validation : `url` doit être `http`/`https` et parsable ; `name` non vide, ≤ 100 caractères.

Exemple de réponse `GET /api/v1/targets` :
```json
[
  {
    "id": 1,
    "name": "Example",
    "url": "https://example.com",
    "interval_s": 30,
    "last_check": {
      "checked_at": "2026-10-18T10:00:00Z",
      "status_code": 200,
      "latency_ms": 123,
      "success": true
    }
  }
]
```

### 3.4 Worker
- Activé si `WORKER_ENABLED=true` (défaut : `true`).
- Toutes les secondes, sélectionne les cibles dont le dernier check date de plus de `interval_s`.
- Exécute les checks en parallèle avec un **pool borné** (`WORKER_CONCURRENCY`, défaut 10).
- Requête `GET` avec timeout (`CHECK_TIMEOUT`, défaut `5s`). Succès = code HTTP 2xx ou 3xx.
- Enregistre le résultat dans `checks` et met à jour les métriques.
- **Arrêt propre** : sur `SIGTERM`, arrête de prendre de nouveaux checks, attend la fin des checks en cours (avec timeout), ferme le pool DB. Indispensable pour Kubernetes.

### 3.5 Métriques Prometheus

| Métrique | Type | Labels |
|---|---|---|
| `upwatch_checks_total` | Counter | `target`, `result` (`success`/`failure`) |
| `upwatch_check_duration_seconds` | Histogram | `target` |
| `upwatch_target_up` | Gauge (0/1) | `target` |
| `upwatch_targets` | Gauge | — |
| `upwatch_http_requests_total` | Counter | `method`, `route`, `code` |
| `upwatch_http_request_duration_seconds` | Histogram | `method`, `route` |

Plus les métriques Go par défaut (`go_*`, `process_*`).
Label `target` = `name` de la cible (cardinalité faible, acceptable pour une démo — à mentionner dans le README).

### 3.6 Configuration (variables d'environnement)

| Variable | Défaut | Description |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Adresse d'écoute |
| `DATABASE_URL` | — (requis) | DSN PostgreSQL |
| `WORKER_ENABLED` | `true` | Active le worker |
| `WORKER_CONCURRENCY` | `10` | Checks simultanés max |
| `CHECK_TIMEOUT` | `5s` | Timeout d'un check |
| `RETENTION` | `168h` | Durée de conservation des checks |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `SHUTDOWN_TIMEOUT` | `15s` | Délai max d'arrêt propre |

### 3.7 Tests attendus
- Handlers HTTP : cas nominaux + validation + 404 (`httptest`, repository mocké via interface).
- Logique de check : succès, code 500, timeout, erreur DNS (serveur `httptest`).
- Calcul d'uptime.
- Cible : `go test ./...` vert, `go vet` et `golangci-lint` propres.

---

## 4. Conteneur

- `Dockerfile` **multi-stage** : build `golang:<version>` → runtime `gcr.io/distroless/static-debian12:nonroot` (ou `scratch`).
- Binaire statique (`CGO_ENABLED=0`, `-ldflags "-s -w"`), version injectée via `-X main.version=…`.
- Utilisateur **non-root**, port 8080.
- Image cible < 25 Mo.
- `docker-compose.yml` pour le dev local rapide (service + PostgreSQL), indépendant du cluster.

---

## 5. Infrastructure

### 5.1 Cluster local
- **k3d**, config déclarative dans `deploy/k3d/cluster.yaml` :
  - 1 server + 2 agents.
  - Port `8080` de l'hôte → load balancer (Ingress Traefik intégré à k3s).
  - Registry local `k3d-registry.localhost:5000` pour pousser l'image sans passer par ghcr.io.

### 5.2 Terraform (`deploy/terraform/`)
Providers : `kubernetes`, `helm` (contexte kube `k3d-upwatch`).

Ressources gérées :
1. Namespaces `upwatch` et `monitoring`.
2. Release Helm **PostgreSQL** (chart Bitnami) dans `upwatch` — mot de passe généré par `random_password`, stocké dans un Secret.
3. Release Helm **kube-prometheus-stack** dans `monitoring` (Prometheus, Grafana, Alertmanager ; config allégée pour tenir en local).
4. Release Helm **upwatch** (chart local `deploy/helm/upwatch`) dans `upwatch`, avec `DATABASE_URL` construit depuis le Secret.

Exigences :
- Versions de providers et de charts **épinglées**.
- `terraform fmt` et `terraform validate` propres.
- State local (`.gitignore`), avec une note dans le README sur un backend distant en vrai contexte.
- Outputs : URL de l'API, URL de Grafana, commande pour récupérer le mot de passe Grafana.

### 5.3 Chart Helm (`deploy/helm/upwatch/`)
- Templates : `Deployment`, `Service`, `Ingress`, `ServiceMonitor`, `ConfigMap`, `HorizontalPodAutoscaler` (désactivable), `PodDisruptionBudget`.
- `values.yaml` documenté : image, réplicas, ressources, variables d'env, ingress host, activation du worker.
- Bonnes pratiques :
  - `livenessProbe` → `/healthz`, `readinessProbe` → `/readyz`.
  - `resources.requests/limits` définis.
  - `securityContext` : `runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`, drop `ALL` capabilities.
  - `terminationGracePeriodSeconds` cohérent avec `SHUTDOWN_TIMEOUT`.
  - Labels standard `app.kubernetes.io/*`.
- `helm lint` propre.

---

## 6. Observabilité

- **ServiceMonitor** pour que Prometheus scrape `/metrics`.
- **Dashboard Grafana** provisionné via ConfigMap (label `grafana_dashboard: "1"`), JSON versionné dans `deploy/grafana/upwatch-dashboard.json`. Panneaux :
  - Statut up/down par cible.
  - Latence p50 / p95 par cible.
  - Taux de succès sur 1 h.
  - Requêtes API par route et code, latence API.
- **PrometheusRule** :
  - `UpwatchTargetDown` : `upwatch_target_up == 0` pendant 2 min → severity `warning`.
  - `UpwatchHighErrorRate` : > 5 % de réponses 5xx sur l'API pendant 5 min → `critical`.

---

## 7. CI — GitHub Actions

Workflow `.github/workflows/ci.yml`, déclenché sur `push` et `pull_request` :

1. **lint** : `golangci-lint`, `terraform fmt -check`, `terraform validate`, `helm lint`.
2. **test** : `go test -race -cover ./...`.
3. **build** : build de l'image Docker (avec cache).
4. **push** (uniquement sur `main` et tags `v*`) : push sur `ghcr.io/ypaquereau/upwatch` avec tags `sha-<short>`, `latest` (main) et `vX.Y.Z` (tag).

Badge de statut CI dans le README.

---

## 8. Developer experience — Makefile

| Cible | Action |
|---|---|
| `make dev` | Lance `docker compose up` (service + Postgres) pour coder vite |
| `make test` | Tests Go |
| `make lint` | Tous les linters |
| `make build` | Build de l'image |
| `make cluster` | Crée le cluster k3d |
| `make push-local` | Build + push sur le registry k3d |
| `make deploy` | `terraform init && terraform apply -auto-approve` |
| `make up` | `cluster` + `push-local` + `deploy` : **tout de zéro en une commande** |
| `make seed` | Ajoute quelques cibles d'exemple via l'API (dont une volontairement en échec pour déclencher l'alerte) |
| `make grafana` | Affiche l'URL et le mot de passe Grafana |
| `make down` | Détruit le cluster |

Prérequis documentés : Docker, k3d, kubectl, Helm, Terraform, Go, make.

---

## 9. Arborescence cible

```
upwatch/
├── cmd/upwatch/main.go          # wiring, config, démarrage, arrêt propre
├── internal/
│   ├── api/                     # handlers HTTP, middleware (logs, métriques)
│   ├── checker/                 # worker + logique de check
│   ├── store/                   # accès PostgreSQL + migrations embarquées
│   ├── metrics/                 # déclaration des métriques
│   └── config/                  # lecture des variables d'env
├── deploy/
│   ├── k3d/cluster.yaml
│   ├── terraform/               # main.tf, providers.tf, variables.tf, outputs.tf
│   ├── helm/upwatch/            # Chart.yaml, values.yaml, templates/
│   └── grafana/upwatch-dashboard.json
├── docs/
│   └── architecture.md          # schéma + décisions (ADR courts)
├── .github/workflows/ci.yml
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── .golangci.yml
├── CLAUDE.md                    # consignes pour Claude Code
└── README.md
```

---

## 10. README (livrable clé)

Sections attendues :
1. Pitch en 2 lignes + badge CI + capture du dashboard Grafana.
2. Schéma d'architecture.
3. Quickstart : prérequis puis `make up`, `make seed`, ouvrir Grafana.
4. Utilisation de l'API (exemples `curl`).
5. **Choix techniques** et pourquoi (stdlib vs framework, binaire unique, k3d, Terraform pour du Helm, etc.).
6. **Ce qui a cassé / ce que j'ai appris** : vrais problèmes rencontrés et leur résolution.
7. **Limites et prochaines étapes** : séparer API et worker, GitOps (Argo CD), cloud managé (Scaleway Kapsule / OKE), backend de state distant, gestion de secrets (External Secrets), tests e2e.

---

## 11. Critères d'acceptation (Definition of Done)

- [ ] `make up` sur une machine Ubuntu propre (avec les prérequis) aboutit à un service accessible sur `http://upwatch.localhost:8080`.
- [ ] `make seed` crée des cibles et des checks apparaissent dans `GET /api/v1/targets`.
- [ ] Le dashboard Grafana affiche les cibles et leurs latences.
- [ ] La cible en échec déclenche l'alerte `UpwatchTargetDown` visible dans Prometheus/Alertmanager.
- [ ] `kubectl rollout restart` du Deployment se fait sans erreur (arrêt propre, readiness OK).
- [ ] CI verte sur `main`, image publiée sur ghcr.io.
- [ ] `make down` supprime tout.
- [ ] README complet selon la section 10.

---

## 12. Planning (week-end)

| Créneau | Livrables |
|---|---|
| **Samedi matin** | Service Go : config, store + migrations, API, worker, métriques, tests. `make dev` fonctionne. |
| **Samedi après-midi** | Dockerfile, cluster k3d, Terraform (namespaces, PostgreSQL, kube-prometheus-stack). |
| **Samedi soir** | Chart Helm upwatch + release Terraform. `make up` fonctionne. |
| **Dimanche matin** | CI GitHub Actions, ServiceMonitor, dashboard Grafana, PrometheusRule. |
| **Dimanche après-midi** | README, schéma, captures, relecture, tag `v0.1.0`. |

Si le temps manque, couper dans cet ordre : HPA/PDB → `testcontainers` → alerte `HighErrorRate` → `docker-compose`.

---

## 13. Consignes pour Claude Code (à reprendre dans `CLAUDE.md`)

- Avancer **étape par étape** selon le planning ; ne pas tout générer d'un coup.
- Après chaque étape : faire tourner `go test ./...`, `golangci-lint run`, `helm lint`, `terraform validate` selon ce qui a été touché.
- Go idiomatique : erreurs wrappées (`fmt.Errorf("…: %w", err)`), `context.Context` propagé partout, pas de variables globales hors métriques, interfaces définies côté consommateur.
- Aucune dépendance ajoutée sans justification courte en commentaire de commit.
- Épingler toutes les versions (images, charts, providers, actions GitHub).
- Commits petits et explicites (Conventional Commits : `feat:`, `fix:`, `chore:`, `docs:`).
- Expliquer brièvement chaque choix non trivial, pour que l'auteur puisse le défendre en entretien.
