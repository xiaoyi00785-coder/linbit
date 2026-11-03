# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/futures/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/futures ./futures
WORKDIR /src/futures
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/futures .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/futures /futures
EXPOSE 8089
ENTRYPOINT ["/futures"]
