# Two-stage build: compile a static binary, then run it on a minimal distroless image.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /moneta ./cmd/moneta

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /moneta /moneta
EXPOSE 8080
ENTRYPOINT ["/moneta"]
