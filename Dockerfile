FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /prometheus-dash .

FROM gcr.io/distroless/static-debian13:nonroot

WORKDIR /app
COPY --from=build /prometheus-dash /usr/local/bin/prometheus-dash
COPY prometheus-dash.yaml ./
ENTRYPOINT ["prometheus-dash"]
