# ---------- Build stage ----------
FROM golang:1.23-alpine AS build

WORKDIR /src

# Copy module files first for caching
COPY go.mod ./
# COPY go.sum ./   # uncomment if/when you add external deps

# Copy the rest of the source (main.go, etc.)
COPY . ./

# Produce a static binary
ENV CGO_ENABLED=0
RUN go build -trimpath -o /mock-soroban ./main.go

# ---------- Runtime stage ----------
FROM alpine:3.20

# Default config (override via Deployment env)
ENV CORE_CONTRACT_ID=CABCYOURCOREID

EXPOSE 8000

COPY --from=build /mock-soroban /mock-soroban
ENTRYPOINT ["/mock-soroban"]