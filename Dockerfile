FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION=dev 
ARG COMMIT=unknown
ARG TARGETPLATFORM

LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.revision=${COMMIT}

RUN export GOOS=$(echo ${TARGETPLATFORM} | cut -d'/' -f1) \
    && export GOARCH=$(echo ${TARGETPLATFORM} | cut -d'/' -f2) \
    && if [ "${GOARCH}" = "arm64" ]; then export GOARCH=arm64; fi \
    && echo "Building for GOOS=${GOOS} GOARCH=${GOARCH}" \
    && CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} \
       go build -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT}" -o bot . \
    && CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} \
       go build -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT}" -o stmpddashboard ./cmd/dashboard \
    && CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} \
       go build -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT}" -o stmpdagent ./cmd/agent

# --- dashboard image -------------------------------------------------------
# Deliberately before the bot stage: buildx defaults to the LAST stage, so
# keeping `bot` last means every existing `docker build .` keeps working.
FROM --platform=$TARGETPLATFORM alpine AS dashboard

RUN apk add --no-cache tzdata

WORKDIR /app

# The binary is built as `stmpddashboard`, not `dashboard`: `go build -o
# dashboard` would write into the repo's existing dashboard/ SOURCE directory
# rather than producing a file, and the COPY below would then copy a directory.
#
# Nothing else is copied in: templates and static assets are go:embed-ed, and
# db/migrations is deliberately absent because the dashboard never migrates --
# the bot owns the schema.
COPY --from=build /build/stmpddashboard /app/dashboard

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/dashboard"]

CMD ["-config", "/var/lib/config.toml"]

# --- agent image -------------------------------------------------------------
# The standalone AI persona service (cmd/agent). Deliberately its own image
# rather than a mode of the bot binary: it is the only container that ever
# holds the LLM API key, and it is the only one that needs to redeploy when a
# prompt, tool or memory change ships.
FROM --platform=$TARGETPLATFORM alpine AS agent

RUN apk add --no-cache tzdata

WORKDIR /app

# SOUL.md and persona.md are go:embed-ed into the binary at build time, so
# nothing else needs to be copied in here.
COPY --from=build /build/stmpdagent /app/agent

EXPOSE 8083

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:8083/health || exit 1

ENTRYPOINT ["/app/agent"]

CMD ["-config", "/var/lib/config.toml"]

# --- bot image (default target) ---------------------------------------------
FROM --platform=$TARGETPLATFORM alpine AS bot

# The Go binary embeds its own copy of the tz database, so the bot does not need
# this. It is here so that anything else running in the container -- a shell, date,
# the healthcheck -- reports the same wall clock the logs do.
RUN apk add --no-cache tzdata

WORKDIR /bot

COPY --from=build /build/bot /bot/stmpdbot
COPY --from=build /build/db/migrations/ /bot/db/migrations/
COPY --from=build /build/assets/ /bot/assets/

EXPOSE 8081

# /health returns 503 unless both Discord and the database are up, which wget
# surfaces as a non-zero exit. start-period covers migrations and the gateway
# handshake so a slow boot is not reported as a failure.
HEALTHCHECK --interval=30s --timeout=5s --start-period=90s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:8081/health || exit 1

ENTRYPOINT ["/bot/stmpdbot"]

CMD ["-config", "/var/lib/config.toml"]