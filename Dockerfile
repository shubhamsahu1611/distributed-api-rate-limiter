FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api-limiter ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api-limiter /api-limiter
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api-limiter"]
