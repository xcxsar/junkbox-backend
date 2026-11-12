# Graph Report - junkbox-backend  (2026-11-09)

## Corpus Check
- 110 files · ~70,744 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 5 file(s) not represented in the graph (top: (none) 4, .template 1)

## Summary
- 1398 nodes · 4323 edges · 70 communities (62 shown, 8 thin omitted)
- Extraction: 91% EXTRACTED · 9% INFERRED · 0% AMBIGUOUS · INFERRED: 370 edges (avg confidence: 0.82)
- Token cost: 201,932 input · 0 output

## Community Hubs (Navigation)
- HTTP Server & DTOs
- Audio Features Worker
- Store Ports & Queries
- Account HTTP Handlers
- Account Email Fakes
- Token & Revocation Stores
- README Concepts & Email Templates
- JWT Access Parsing
- Envoy Gateway Config
- Auth Service Tests
- K8s Backend Deployment
- Ratings Store
- Email Outbox Worker
- User Creation & Nullables
- Catalog Trigram Queries
- Ratings & Reviews Handlers
- Music Search Ports
- Router & CORS
- Reviews Store
- Docker Compose Services
- Config Loading
- Retry-After Errors
- Music Service Tests
- SQLC Config
- K8s ConfigMap
- FreqBlog Client
- Auth Design (Docs)
- K8s Migrate Job
- Account Email Tests
- Rating/Review Service Tests
- Server Startup & Purge
- Spotify Client Tests
- K8s Service
- Replica 1 Overlay
- Replica 2 Overlay
- Replica 3 Overlay
- Replica 4 Overlay
- Bcrypt Hasher
- DB Transactions
- GatewayClass Config
- Catalog Importer Helpers
- JWT & Hasher Tests
- Kind Cluster Config
- Album Import & Search
- Music Catalog Design (Docs)
- Data Infrastructure (Docs)
- Workers & Deployment (Docs)
- Stack Overview (Docs)
- Refresh Tokens
- Kind Compose Network
- Email Template Rendering
- Track Import & Search
- Auth Service Wiring
- FreqBlog Client Tests
- Ratings & Reviews Design (Docs)
- Spotify Object Mapping
- CLI Entrypoint
- Artist Import & Search
- SMTP Mailer
- Get Album Query
- Artist Trigram Search
- Backend Kustomization
- Base Kustomization
- K8s Namespace
- Migrate Kustomization
- Root Kustomization
- Email Token Purpose Enum
- Gateway & Router (Docs)
- Unique Violation Fake
- Go Module

## God Nodes (most connected - your core abstractions)
1. `Querier` - 60 edges
2. `User` - 38 edges
3. `newEnv()` - 31 edges
4. `runServer()` - 29 edges
5. `AccessClaims` - 26 edges
6. `writeJSON()` - 24 edges
7. `writeError()` - 24 edges
8. `Config` - 23 edges
9. `writeServiceError()` - 23 edges
10. `AudioFeaturesWorker` - 20 edges

## Surprising Connections (you probably didn't know these)
- `Email Verification Plain-Text Template` --implements--> `Email Verification Flow`  [INFERRED]
  internal/core/services/templates/email_verification.txt → README.md
- `Email HTML Layout Template` --conceptually_related_to--> `email_outbox Transactional Outbox`  [INFERRED]
  internal/core/services/templates/layout.html → README.md
- `runServer()` --calls--> `Load()`  [EXTRACTED]
  cmd/server.go → config/config.go
- `runServer()` --calls--> `NewAccountHandler()`  [EXTRACTED]
  cmd/server.go → internal/adapters/primary/http/handlers_account.go
- `runServer()` --calls--> `NewAuthHandler()`  [EXTRACTED]
  cmd/server.go → internal/adapters/primary/http/handlers_auth.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **JWT session security and revocation flow** — implementacion_authservice, implementacion_refresh_tokens, implementacion_revoked_access_tokens, implementacion_user_access_revocations, implementacion_auth_middleware, implementacion_jwt_hs256 [EXTRACTED 1.00]
- **Search cache-through import and async embedding pipeline** — implementacion_musicservice, implementacion_spotify_client, implementacion_catalog_importer, implementacion_audio_features_worker, implementacion_freqblog_client, implementacion_song_metadata [EXTRACTED 1.00]
- **SKIP LOCKED coordination for multi-replica safety** — implementacion_lease_claim, implementacion_email_outbox, implementacion_artist_claim_rule, implementacion_kind_cluster [INFERRED 0.85]
- **Auth Token Lifecycle (JWT + refresh rotation + denylist)** — readme_jwt_access_tokens, readme_refresh_token_rotation, readme_token_denylist, readme_auth_endpoints [EXTRACTED 1.00]
- **Email Delivery Pipeline** — readme_email_outbox, readme_mail_driver, readme_mailpit, readme_resend, internal_core_services_templates_layout_layout, internal_core_services_templates_email_verification_content, internal_core_services_templates_email_verification_text_body [INFERRED 0.85]
- **Music Search with Spotify Fallback** — readme_pg_trgm_fuzzy_search, readme_search_fallback_threshold, readme_spotify_fallback, readme_spotify_client_credentials, readme_pending_embedding, readme_freqblog [EXTRACTED 1.00]

## Communities (70 total, 8 thin omitted)

### Community 0 - "HTTP Server & DTOs"
Cohesion: 0.14
Nodes (17): ValidationError, CreateReviewRequest, ErrorResponse, ForgotPasswordRequest, LoginRequest, LogoutRequest, MessageResponse, RatingRequest (+9 more)

### Community 1 - "Audio Features Worker"
Cohesion: 0.07
Nodes (34): AudioFeatures, EmbeddingJob, AudioFeaturesProvider, EmbeddingStore, BuildEmbedding(), FormatVector(), NewAudioFeaturesWorker(), orMedian() (+26 more)

### Community 2 - "Store Ports & Queries"
Cohesion: 0.06
Nodes (9): RevokeAccessTokenParams, CountRecentEmailTokensParams, ExistsUserByEmailOrUsernameParams, ConsumeEmailTokenParams, EnqueueEmailParams, InsertEmailTokenParams, InvalidateEmailTokensParams, Querier (+1 more)

### Community 3 - "Account HTTP Handlers"
Cohesion: 0.12
Nodes (27): AccountHandler, AuthHandler, MusicHandler, RatingHandler, ReviewHandler, RouterOptions, AuthResponse, authResponse() (+19 more)

### Community 4 - "Account Email Fakes"
Cohesion: 0.07
Nodes (16): accountFakes, consume(), enqueue(), Email, EmailTokenPurpose, IssuedEmailToken, newFakeAccountTx(), User (+8 more)

### Community 5 - "Token & Revocation Stores"
Cohesion: 0.07
Nodes (10): RevocationStore, TokenStore, SetRefreshTokenReplacedByParams, CreateReviewParams, fakeRevocationStore, fakeRevocationStore, fakeTokenStore, RevokedAccessToken (+2 more)

### Community 6 - "README Concepts & Email Templates"
Cohesion: 0.06
Nodes (43): Email Verification HTML Template (content block), Email Verification Plain-Text Template, Email HTML Layout Template, Auth Endpoints (/auth/*), CORS_ALLOWED_ORIGINS, CSV Ingest Pipeline (ingest_songs.sql), email_outbox Transactional Outbox, 3 Email Links per Hour Limit (+35 more)

### Community 7 - "JWT Access Parsing"
Cohesion: 0.10
Nodes (28): ExpiredAccessParser, expiredAccessValidator, staticJWT, NewAuthHandler(), AuthMiddleware(), ClaimsFromRequest(), JWTValidator, NewRevocationChecker() (+20 more)

### Community 8 - "Envoy Gateway Config"
Cohesion: 0.06
Nodes (36): name, port, EnvoyProxy kind-nodeport, Gateway junkbox, HTTPRoute backend, apiVersion, kind, metadata (+28 more)

### Community 9 - "Auth Service Tests"
Cohesion: 0.17
Nodes (34): TestRegisterEnviaVerificacion(), TestRegisterNoFallaSiLaVerificacionFalla(), hashToken(), mustLogin(), newEnv(), newFakeRevocationStore(), newFakeTokenStore(), newFakeUserStore() (+26 more)

### Community 10 - "K8s Backend Deployment"
Cohesion: 0.06
Nodes (33): apiVersion, envFrom, image, imagePullPolicy, livenessProbe, name, ports, readinessProbe (+25 more)

### Community 11 - "Ratings Store"
Cohesion: 0.11
Nodes (12): RatingStore, eqUUID(), toUpsertRow(), Rating, GetUserRatingParams, UpsertAlbumRatingParams, UpsertAlbumRatingRow, UpsertArtistRatingParams (+4 more)

### Community 12 - "Email Outbox Worker"
Cohesion: 0.10
Nodes (14): OutboxQueries, OutboxStore, NewEmailOutboxWorker(), newFakeOutbox(), TestOutboxEnviaYMarca(), TestOutboxProcesaVariosLotes(), TestOutboxReintentaConBackoffYFalla(), ClaimDueEmailsRow (+6 more)

### Community 13 - "User Creation & Nullables"
Cohesion: 0.11
Nodes (12): CreateUserParams, CreateUserRow, Album, Artist, ClaimArtistByNameParams, EmailOutbox, GetSongByTrackIDRow, Review (+4 more)

### Community 14 - "Catalog Trigram Queries"
Cohesion: 0.12
Nodes (10): GetArtistParams, GetArtistRow, GetTrackParams, GetTrackRow, ListTracksByAlbumRow, SearchAlbumsTrigramParams, SearchAlbumsTrigramRow, SearchTracksTrigramParams (+2 more)

### Community 15 - "Ratings & Reviews Handlers"
Cohesion: 0.13
Nodes (18): ReviewAuthor, NewRatingHandler(), Rating, Review, ReviewPage, Target, TargetRef, TargetResolver (+10 more)

### Community 16 - "Music Search Ports"
Cohesion: 0.14
Nodes (18): SearchResult, NewValidationError(), CatalogImporter, CatalogStore, EmbeddingQueue, MusicProvider, stringPtr(), albumFromRow() (+10 more)

### Community 17 - "Router & CORS"
Cohesion: 0.15
Nodes (19): NewAccountHandler(), newAccountRouter(), post(), TestForgotPasswordRespuestaUniforme(), TestForgotPasswordSinEmail(), TestResendVerificationRequiereBearer(), TestResetPasswordCodigos(), TestVerifyEmailTokenInvalido() (+11 more)

### Community 18 - "Reviews Store"
Cohesion: 0.12
Nodes (7): ReviewStore, CountReviewsByTargetParams, GetReviewByIDRow, ListReviewsByTargetParams, ListReviewsByTargetRow, UpdateReviewParams, fakeReviews

### Community 19 - "Docker Compose Services"
Cohesion: 0.08
Nodes (23): interval, retries, start_period, test, timeout, container_name, image, ports (+15 more)

### Community 20 - "Config Loading"
Cohesion: 0.17
Nodes (15): durationOr(), envOr(), floatOr(), Config, intOr(), listOr(), Load(), setRequired() (+7 more)

### Community 21 - "Retry-After Errors"
Cohesion: 0.14
Nodes (7): APIError, TokenManager, RetryAfterError, AudioFeaturesWorkerConfig, fakeJWT, rateLimitErr, APIError

### Community 22 - "Music Service Tests"
Cohesion: 0.20
Nodes (20): localTrack(), newMusicFixture(), spotifyTrack(), TestGetAlbumIncluyePistas(), TestGetArtistResuelveUUIDOSpotify(), TestGetTrackIDInvalidoYNoEncontrado(), TestGetTrackPorIDLocalYSpotify(), TestGetTrackSinAudioFeaturesDevuelveNull() (+12 more)

### Community 23 - "SQLC Config"
Cohesion: 0.10
Nodes (19): go, emit_exact_table_names, emit_interface, emit_json_tags, emit_prepared_queries, json_tags_case_style, out, overrides (+11 more)

### Community 24 - "K8s ConfigMap"
Cohesion: 0.11
Nodes (18): apiVersion, data, ACCESS_TOKEN_TTL, CORS_ALLOWED_ORIGINS, EMAIL_VERIFICATION_TTL, FRONTEND_URL, HTTP_ADDR, MAIL_DRIVER (+10 more)

### Community 25 - "FreqBlog Client"
Cohesion: 0.13
Nodes (9): Client, Config, features, errorDetail(), New(), errorMessage(), New(), Client (+1 more)

### Community 26 - "Auth Design (Docs)"
Cohesion: 0.16
Nodes (16): AccountEmailService, AUTH-02 password recovery and email verification, AuthMiddleware + RevocationChecker (fail-closed), Auth ports (UserStore, TokenStore, RevocationStore, PasswordHasher, TokenManager), AuthService, bcrypt hasher (cost 12), cmd/server.go (wiring, purge goroutine, graceful shutdown), .env.template configuration (+8 more)

### Community 27 - "K8s Migrate Job"
Cohesion: 0.12
Nodes (16): apiVersion, env, image, imagePullPolicy, name, kind, metadata, name (+8 more)

### Community 28 - "Account Email Tests"
Cohesion: 0.24
Nodes (15): newAccountEnv(), TestForgotPasswordEmailInexistente(), TestForgotPasswordEnviaEnlace(), TestForgotPasswordLimitePorHora(), TestResendVerification(), TestResendVerificationUsuarioBorrado(), TestResetPasswordInvalidaNoConsumeToken(), TestResetPasswordOK() (+7 more)

### Community 29 - "Rating/Review Service Tests"
Cohesion: 0.23
Nodes (16): ptr(), TestBuildEmbeddingAcotaACeroUno(), NewRatingService(), NewReviewService(), newFakeRatings(), newFakeReviews(), TestCreateReviewDuplicada(), TestCreateReviewNormaliza() (+8 more)

### Community 30 - "Server Startup & Purge"
Cohesion: 0.18
Nodes (13): fatalf(), purge(), purgeRevokedTokens(), runServer(), NewMusicHandler(), NewReviewHandler(), NewAccountTx(), NewCatalogImporter() (+5 more)

### Community 31 - "Spotify Client Tests"
Cohesion: 0.22
Nodes (8): TestCredencialesInvalidas(), TestSearch401RenuevaTokenUnaVez(), TestSearch429ConRetryAfterCortoReintenta(), TestSearch429ConRetryAfterLargoFalla(), TestSearchPaginaDeDiezEnDiez(), TestSearchTracksMapeaYCacheaToken(), TestTokenPorExpirarSeRenueva(), fakeSpotify

### Community 32 - "K8s Service"
Cohesion: 0.15
Nodes (12): apiVersion, kind, metadata, name, name, port, targetPort, app (+4 more)

### Community 33 - "Replica 1 Overlay"
Cohesion: 0.15
Nodes (12): apiVersion, kind, labels, includeSelectors, pairs, nameSuffix, replica, patches (+4 more)

### Community 34 - "Replica 2 Overlay"
Cohesion: 0.15
Nodes (12): apiVersion, kind, labels, includeSelectors, pairs, nameSuffix, replica, patches (+4 more)

### Community 35 - "Replica 3 Overlay"
Cohesion: 0.15
Nodes (12): apiVersion, kind, labels, includeSelectors, pairs, nameSuffix, replica, patches (+4 more)

### Community 36 - "Replica 4 Overlay"
Cohesion: 0.15
Nodes (12): apiVersion, kind, labels, includeSelectors, pairs, nameSuffix, replica, patches (+4 more)

### Community 37 - "Bcrypt Hasher"
Cohesion: 0.18
Nodes (3): plainHasher, Hasher, fakeHasher

### Community 38 - "DB Transactions"
Cohesion: 0.19
Nodes (4): runInTx(), Queries, New(), DBTX

### Community 39 - "GatewayClass Config"
Cohesion: 0.17
Nodes (11): apiVersion, kind, metadata, name, group, kind, name, namespace (+3 more)

### Community 40 - "Catalog Importer Helpers"
Cohesion: 0.36
Nodes (9): int32Ptr(), nonEmpty(), nullInt32(), nullString(), stringPtr(), upsertAlbum(), upsertArtist(), wrapArtist() (+1 more)

### Community 41 - "JWT & Hasher Tests"
Cohesion: 0.27
Nodes (11): NewHasher(), NewJWTManager(), TestGenerateAccessJTIUnico(), TestGenerateAndParseAccess(), TestHasher(), TestParseAccessAllowExpiredAceptaVencido(), TestParseAccessAllowExpiredRechazaFirmaInvalida(), TestParseAccessExpirado() (+3 more)

### Community 42 - "Kind Cluster Config"
Cohesion: 0.18
Nodes (10): apiVersion, containerPort, hostPort, listenAddress, protocol, kind, name, nodes (+2 more)

### Community 43 - "Album Import & Search"
Cohesion: 0.31
Nodes (5): RatingStats, Track, Album, ImportedAlbum, TrackDetail

### Community 44 - "Music Catalog Design (Docs)"
Cohesion: 0.27
Nodes (8): CatalogImporter (transactional Spotify import), db/queries/catalog.sql, Music HTTP endpoints (/api/v1/music/*), Music ports (CatalogStore, CatalogImporter, MusicProvider, AudioFeaturesProvider, EmbeddingQueue), MusicService, pg_trgm strict_word_similarity search (GIN trigram indexes), Spotify client (Client Credentials, MusicProvider), SQLC code generation (sqlc.yaml)

### Community 45 - "Data Infrastructure (Docs)"
Cohesion: 0.33
Nodes (9): artists and albums catalog tables, backfill_artists.sql, CSV ingestion pipeline (db/scripts/ingest_songs.sql), Goose migrations, IVFFlat vector index (idx_song_metadata_embedding_ivfflat), Makefile automation (db-reset safeguard, ingest, seed, k8s), Kubernetes migrate Job (goose up), song_metadata table (+1 more)

### Community 46 - "Workers & Deployment (Docs)"
Cohesion: 0.24
Nodes (7): Audio features embedding worker, Multi-stage Dockerfile (distroless server + migrate targets), Exponential backoff retries (30s * 2^n, max 5), FreqBlog client (AudioFeaturesProvider), Multi-replica Kubernetes deployment on kind (4 replicas), Median imputation of null audio features, Resend via generic SMTP (production)

### Community 47 - "Stack Overview (Docs)"
Cohesion: 0.22
Nodes (7): db/docker-compose.yml (Postgres pgvector + Mailpit), 10-dim song embedding with MinMax normalization, Junkbox Backend (Go), Mailpit (dev SMTP), pgvector, Docker volume postgres-junkbox-data, t-box-backend (base project)

### Community 49 - "Kind Compose Network"
Cohesion: 0.22
Nodes (8): external, networks, networks, kind, networks, services, mailpit, postgres

### Community 50 - "Email Template Rendering"
Cohesion: 0.22
Nodes (6): TestHumanDuration(), greetingName(), humanDuration(), mustEmailTemplate(), plural(), emailTemplate

### Community 51 - "Track Import & Search"
Cohesion: 0.39
Nodes (4): ImportedSong, ImportedTrack, fakeImporter, fakeProvider

### Community 52 - "Auth Service Wiring"
Cohesion: 0.39
Nodes (8): PasswordHasher, UserStore, AccountTx, EmailTokenStore, AccountEmailService, NewAccountEmailService(), NewAuthService(), AccountEmailConfig

### Community 53 - "FreqBlog Client Tests"
Cohesion: 0.39
Nodes (5): newTestClient(), TestGetAudioFeatures401(), TestGetAudioFeatures429DevuelveRetryAfter(), TestGetAudioFeaturesDivideEnLotesDe100(), TestGetAudioFeaturesMapeaYOmiteNulos()

### Community 54 - "Ratings & Reviews Design (Docs)"
Cohesion: 0.39
Nodes (7): Ratings and Reviews HTTP endpoints, Ratings and Reviews module (WBS 1.3.3/1.3.4), ratings table (0.5-5.0 half stars), RatingService, MusicService.ResolveTarget (TargetResolver), reviews table, ReviewService

### Community 55 - "Spotify Object Mapping"
Cohesion: 0.43
Nodes (5): largestImage(), albumObject, artistObject, image, trackObject

### Community 58 - "SMTP Mailer"
Cohesion: 0.40
Nodes (5): newMailer(), New(), TestNewValidaConfig(), Config, Mailer

### Community 61 - "Backend Kustomization"
Cohesion: 0.40
Nodes (4): apiVersion, kind, namespace, resources

### Community 62 - "Base Kustomization"
Cohesion: 0.40
Nodes (4): apiVersion, kind, namespace, resources

### Community 63 - "K8s Namespace"
Cohesion: 0.40
Nodes (4): apiVersion, kind, metadata, name

### Community 64 - "Migrate Kustomization"
Cohesion: 0.40
Nodes (4): apiVersion, kind, namespace, resources

### Community 65 - "Root Kustomization"
Cohesion: 0.50
Nodes (3): apiVersion, kind, resources

### Community 67 - "Gateway & Router (Docs)"
Cohesion: 0.50
Nodes (4): Chi router with CORS (router.go), cURL verification guide, Envoy Gateway + Gateway API (HTTPRoute backend), X-Instance response header

## Knowledge Gaps
- **15 isolated node(s):** `github.com/xcxsar/junkbox-backend`, `Median imputation of null audio features`, `Resend via generic SMTP (production)`, `cURL verification guide`, `Multi-stage Dockerfile (distroless server + migrate targets)` (+10 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 263 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **8 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Config` connect `Config Loading` to `HTTP Server & DTOs`, `SMTP Mailer`, `CLI Entrypoint`, `Retry-After Errors`?**
  _High betweenness centrality (0.013) - this node is a cross-community bridge._
- **What connects `github.com/xcxsar/junkbox-backend`, `Median imputation of null audio features`, `Resend via generic SMTP (production)` to the rest of the system?**
  _15 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `HTTP Server & DTOs` be split into smaller, more focused modules?**
  _Cohesion score 0.13966770508826584 - nodes in this community are weakly interconnected._
- **Why does `AccessClaims` connect `JWT Access Parsing` to `HTTP Server & DTOs`, `Account HTTP Handlers`, `Account Email Fakes`, `Token & Revocation Stores`, `Auth Service Tests`?**
  _High betweenness centrality (0.011) - this node is a cross-community bridge._
- **Should `Audio Features Worker` be split into smaller, more focused modules?**
  _Cohesion score 0.0679563492063492 - nodes in this community are weakly interconnected._
- **Why does `runServer()` connect `Server Startup & Purge` to `HTTP Server & DTOs`, `Audio Features Worker`, `DB Transactions`, `JWT Access Parsing`, `JWT & Hasher Tests`, `Email Outbox Worker`, `Ratings & Reviews Handlers`, `Music Search Ports`, `Router & CORS`, `Config Loading`, `Auth Service Wiring`, `FreqBlog Client`, `SMTP Mailer`, `Rating/Review Service Tests`?**
  _High betweenness centrality (0.011) - this node is a cross-community bridge._
- **Should `Store Ports & Queries` be split into smaller, more focused modules?**
  _Cohesion score 0.060285563194077206 - nodes in this community are weakly interconnected._