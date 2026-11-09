# GO_VERSION must stay equal to the toolchain line in go.mod (scripts/check-docs.sh checks it).
ARG GO_VERSION=1.27.3
# One image: the meterwised binary plus the built web app (decision D1).
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
# The publishable key is public (it ships in the bundle) but required: build:release fails without it.
ARG VITE_CLERK_PUBLISHABLE_KEY
COPY web/ ./
RUN npm run build:release

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT" -o /out/meterwised ./cmd/meterwised

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/meterwised /app/meterwised
COPY --from=web /web/dist /app/web
ENV WEB_DIR=/app/web
ENV APP_ENV=prod
# 8080 proxy, 8081 control plane and web app; the admin listener (9090) stays private.
EXPOSE 8080 8081
ENTRYPOINT ["/app/meterwised"]
CMD ["serve"]
