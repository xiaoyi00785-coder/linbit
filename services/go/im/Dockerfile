# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/im/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/im ./im
WORKDIR /src/im
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/im .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/im /im
EXPOSE 8096
ENTRYPOINT ["/im"]
