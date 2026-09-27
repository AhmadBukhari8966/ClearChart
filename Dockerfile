# Two-stage build: templates and static files are embedded in the binary.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /clearchart .

# Distroless static includes CA certificates for Supabase TLS.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /clearchart /clearchart
# Non-secret deploy settings. The app loads ./.env at startup without
# overriding real environment variables, so Fly secrets still take precedence.
COPY deploy.env /app/.env
ENV HOST=0.0.0.0 PORT=8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/clearchart"]
