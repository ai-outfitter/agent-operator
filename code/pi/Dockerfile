FROM docker.io/library/node:24.20.0-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git gh ripgrep curl jq \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/pi
COPY package.json package-lock.json ./
RUN npm ci --omit=dev \
    && ln -s /opt/pi/node_modules/.bin/pi /usr/local/bin/pi \
    && npm cache clean --force
COPY verify-native-mcp.mjs ./
RUN node verify-native-mcp.mjs
ENV NODE_PATH=/opt/pi/node_modules
USER 1000:1000
ENTRYPOINT ["pi"]
