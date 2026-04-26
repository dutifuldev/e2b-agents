FROM node:22-bookworm AS node-deps
WORKDIR /app
COPY package.json package-lock.json* tsconfig.json ./
COPY runtime/e2b-helper ./runtime/e2b-helper
RUN npm install
RUN npm run build

FROM golang:1.26-bookworm AS go-build
WORKDIR /src
ENV CGO_CFLAGS="-O0"
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=node-deps /app/runtime/e2b-helper/dist ./runtime/e2b-helper/dist
RUN go build -o /out/e2b-agents ./cmd/e2b-agents

FROM node:22-bookworm
WORKDIR /app
ENV APP_ADDR=:8080
ENV E2B_HELPER_SCRIPT=/app/runtime/e2b-helper/dist/helper.js
COPY package.json package-lock.json* ./
RUN npm install --omit=dev
COPY --from=node-deps /app/runtime/e2b-helper/dist ./runtime/e2b-helper/dist
COPY --from=go-build /out/e2b-agents /usr/local/bin/e2b-agents
COPY migrations ./migrations
EXPOSE 8080
ENTRYPOINT ["e2b-agents"]
CMD ["serve"]
