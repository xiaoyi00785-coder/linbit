# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/growth/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/growth ./growth
WORKDIR /src/growth
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/growth .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/growth /growth
EXPOSE 8091
ENTRYPOINT ["/growth"]
