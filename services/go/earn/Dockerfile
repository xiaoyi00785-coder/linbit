# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/earn/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/earn ./earn
WORKDIR /src/earn
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/earn .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/earn /earn
EXPOSE 8092
ENTRYPOINT ["/earn"]
