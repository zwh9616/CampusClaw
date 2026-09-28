# Build context is the repository root so this file can reach deploy/nginx.conf
# as well as the frontend sources.
#
#   docker build -f frontend/Dockerfile .

# --- build -------------------------------------------------------------------
FROM node:22-alpine AS build

WORKDIR /src

# Copy the manifest and lockfile alone first so npm ci is cached until the
# dependency set actually changes.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY frontend/ ./
RUN npm run build

# --- runtime -----------------------------------------------------------------
FROM nginx:1.27-alpine

COPY --from=build /src/dist /usr/share/nginx/html
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
