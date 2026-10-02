FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /prom-dash .

FROM gcr.io/distroless/static-debian13:nonroot

WORKDIR /app
COPY --from=build /prom-dash /usr/local/bin/prom-dash
COPY prom-dash.yaml ./
ENTRYPOINT ["prom-dash"]
