# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/topo ./cmd/topo

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/topo /topo
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/topo"]
CMD ["serve"]

