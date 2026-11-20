# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/options/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/options ./options
WORKDIR /src/options
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/options .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/options /options
EXPOSE 8096
ENTRYPOINT ["/options"]
