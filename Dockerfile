# GO_VERSION must stay equal to the toolchain line in go.mod (scripts/check-docs.sh checks it).
ARG GO_VERSION=1.27.4
# One image: the meterwised binary plus the built web app (decision D1).
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
# The publishable key is public (it ships in the bundle) but required: build:release fails without it.
ARG VITE_CLERK_PUBLISHABLE_KEY
COPY web/ ./
RUN npm run build:release \
 && find dist -name '*.map' -delete \
 && ! find dist -name '*.map' | grep -q .

# Dev-only bundle for the local Docker stack (make up): the mock sign-in adapter is compiled in on purpose and
# points at the Clerk twin's token mint. Never a dependency of the release stage below.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web-dev
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
ARG VITE_MOCK_TOKEN_URL=http://127.0.0.1:9444/mint
COPY web/ ./
RUN NODE_ENV=development npx vite build --mode development --outDir dist \
 && find dist -name '*.map' -delete

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT" -o /out/meterwised ./cmd/meterwised

# The Clerk twin (cmd/clerkmock-serve), dev stack only; the release image never contains it.
FROM build AS build-mock
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" \
    -o /out/clerkmock-serve ./cmd/clerkmock-serve

# Targets `dev` and `clerkmock` are for `make up` only (docker-compose.yml, profile app). Non-root uid 10001.
FROM alpine:3.24 AS dev
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 meterwise && mkdir /ca && chown meterwise /ca
COPY --from=build /out/meterwised /app/meterwised
COPY --from=web-dev /web/dist /app/web
COPY docker/dev-entrypoint.sh /app/dev-entrypoint.sh
ENV WEB_DIR=/app/web APP_ENV=dev
USER meterwise
EXPOSE 8080 8081
HEALTHCHECK --interval=5s --timeout=3s --retries=20 CMD wget -qO- http://127.0.0.1:8081/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/dev-entrypoint.sh"]
CMD ["serve"]

FROM alpine:3.24 AS clerkmock
RUN adduser -D -u 10001 meterwise && mkdir /ca && chown meterwise /ca
COPY --from=build-mock /out/clerkmock-serve /app/clerkmock-serve
USER meterwise
EXPOSE 9443 9444
HEALTHCHECK --interval=3s --timeout=3s --retries=20 \
  CMD test -s /ca/ca.pem && wget -qO- "http://127.0.0.1:9444/mint?email=healthcheck@meterwise.local" >/dev/null || exit 1
ENTRYPOINT ["/app/clerkmock-serve"]

# Release image: must stay the LAST stage so a plain `docker build .` produces it.
# No shell in distroless, so no HEALTHCHECK here: orchestrators probe GET :8081/healthz.
FROM gcr.io/distroless/static-debian12:nonroot AS release
COPY --from=build /out/meterwised /app/meterwised
COPY --from=web /web/dist /app/web
ENV WEB_DIR=/app/web
ENV APP_ENV=prod
# 8080 proxy, 8081 control plane and web app; the admin listener (9090) stays private.
EXPOSE 8080 8081
ENTRYPOINT ["/app/meterwised"]
CMD ["serve"]
