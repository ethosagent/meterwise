# One image: the meterwised binary plus the built web app (decision D1).
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev COMMIT=unknown
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT" -o /out/meterwised ./cmd/meterwised

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/meterwised /app/meterwised
COPY --from=web /web/dist /app/web
EXPOSE 8080
ENTRYPOINT ["/app/meterwised"]
CMD ["serve"]
