# 1.27.3 trixie
FROM golang@sha256:8aa2821993207ae840ce09e4223217efcd14e01f71525db734faf4232bd59999  AS builder

ARG VERSION
ENV PKG=github.com/DavidHernandez21/kubernetes-event-exporter/pkg

COPY . /app
WORKDIR /app
RUN CGO_ENABLED=0 GOOS=linux GO11MODULE=on go build -pgo=/app/default.pgo -ldflags="-s -w -X ${PKG}/version.Version=${VERSION}" -a -o /main .

# nonroot
FROM gcr.io/distroless/static@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=builder --chown=nonroot:nonroot /main /kubernetes-event-exporter

# https://github.com/GoogleContainerTools/distroless/blob/main/base/base.bzl#L8C1-L9C1
USER 65532

ENTRYPOINT ["/kubernetes-event-exporter"]
