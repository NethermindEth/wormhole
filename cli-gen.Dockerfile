# syntax=docker/dockerfile:1.3
FROM node:22.16-alpine3.22 AS cli-build

# node-gyp toolchain for native deps (bufferutil, etc.)
RUN apk add --no-cache python3 make g++

# Let npm/node-gyp find python explicitly (helps on some CI)
ENV npm_config_python=/usr/bin/python3

# Copy lockfiles first for better caching
WORKDIR /clients/js
COPY clients/js/package.json clients/js/package-lock.json ./
RUN npm ci

# Copy source and build CLI
COPY clients/js ./
RUN npm run build

FROM scratch AS cli-export
COPY --from=cli-build /clients/js/build/main.js /clients/js/build/main.js
COPY --from=cli-build /clients/js/package.json /clients/js/package.json
