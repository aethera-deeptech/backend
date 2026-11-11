# The build runs on the machine that builds the image and cross-compiles for the target, so an arm64 image needs no emulation
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/backend ./cmd/api

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

COPY --from=build /out/backend /backend

EXPOSE 8080

USER 65532:65532

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/backend", "healthcheck"]

ENTRYPOINT ["/backend"]
