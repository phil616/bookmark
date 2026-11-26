# 静态前端镜像：只提供 web/dist，不含后端。API 地址在构建时写入产物。
# 用法：docker build -t online-bookmarks-web --build-arg VITE_API_BASE_URL=https://api.example.com ./web
FROM node:24-alpine AS build
WORKDIR /web
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VITE_API_BASE_URL=
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
RUN npm run build

FROM nginx:1.27-alpine
LABEL org.opencontainers.image.title="online-bookmarks-web" \
      org.opencontainers.image.version="1.0.0" \
      org.opencontainers.image.authors="phil616" \
      org.opencontainers.image.licenses="MIT"
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /web/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1/ || exit 1
