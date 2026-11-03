# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/admin/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/admin ./admin
WORKDIR /src/admin
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/admin .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/admin /admin
EXPOSE 8088
ENTRYPOINT ["/admin"]
