# syntax=docker/dockerfile:1

FROM golang:1.27.1-alpine AS go-build
WORKDIR /src
COPY go.mod ./
COPY backend/ ./backend/
COPY mock-service/*.go mock-service/plans.json ./mock-service/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./backend && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mock ./mock-service

FROM alpine:3.22 AS api
RUN apk add --no-cache ca-certificates && addgroup -S buddy && adduser -S -G buddy buddy
WORKDIR /app
COPY --from=go-build /out/api /app/api
COPY backend/materials.json /app/materials.json
USER buddy
ENV API_ADDR=:8080 MATERIALS_CATALOG_PATH=/app/materials.json
EXPOSE 8080
CMD ["/app/api"]

FROM alpine:3.22 AS mock
RUN addgroup -S buddy && adduser -S -G buddy buddy && mkdir -p /data && chown buddy:buddy /data
COPY --from=go-build /out/mock /app/mock
USER buddy
ENV MOCK_SERVICE_ADDR=:8081 MOCK_DATA_DIR=/data
EXPOSE 8081
VOLUME ["/data"]
CMD ["/app/mock"]

FROM node:24-alpine AS web-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/index.html frontend/tsconfig*.json frontend/vite.config.ts ./
COPY frontend/src/ ./src/
COPY frontend/public/ ./public/
RUN npm run build

FROM nginx:1.29-alpine AS web
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=web-build /src/frontend/dist/ /usr/share/nginx/html/
EXPOSE 80
