# Stage 1: Build frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.24-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /app/frontend/dist /app/frontend-dist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/lingma2api .

# Stage 3: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend-builder /app/lingma2api .
COPY config.yaml .
RUN mkdir -p auth
EXPOSE 8080
ENTRYPOINT ["./lingma2api"]
CMD ["-config", "./config.yaml"]
