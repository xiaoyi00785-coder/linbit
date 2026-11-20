# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/launchpad/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/launchpad ./launchpad
WORKDIR /src/launchpad
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/launchpad .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/launchpad /launchpad
EXPOSE 8096
ENTRYPOINT ["/launchpad"]
