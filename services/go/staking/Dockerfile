# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/staking/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/staking ./staking
WORKDIR /src/staking
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/staking .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/staking /staking
EXPOSE 8095
ENTRYPOINT ["/staking"]
