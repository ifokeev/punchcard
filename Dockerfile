# syntax=docker/dockerfile:1
# Build the static punch binary, then ship it on distroless.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /punch .

FROM gcr.io/distroless/static-debian12
COPY --from=build /punch /usr/local/bin/punch
# tasks.json / memory.json / control.json / artifacts live here — mount a volume to persist.
WORKDIR /data
# serve reads PORT + PUNCH_TOKEN from the env. PORT=8080 makes it bind 0.0.0.0:8080 in the
# container (a token is then required — pass -e PUNCH_TOKEN=...). Platforms that inject
# their own PORT (Render/Fly) override this.
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/punch", "serve"]
