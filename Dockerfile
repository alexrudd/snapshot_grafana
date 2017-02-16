#  snapshot_grafana
## Cli tool for taking Grafana dashboard snapshots
FROM scratch
MAINTAINER Alex Rudd <github.com/AlexRudd/key-manager/issues>

# Get ca-certificates.crt file for https requests
# Fails in Docker 1.12.x, waiting for 1.13.x (https://github.com/docker/docker/issues/28694#issuecomment-280358399)
# ADD https://curl.haxx.se/ca/cacert.pem /etc/ssl/certs/ca-certificates.crt
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Add executable
COPY snapshot_grafana /

ENTRYPOINT ["/snapshot_grafana"]
