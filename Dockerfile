FROM oven/bun:1.2

WORKDIR /app

# Copy package files
COPY package.json bun.lock* ./

# Install dependencies
RUN bun install

# Copy source code
COPY . .

# Run the application
CMD ["bun", "run", "src/index.ts"]
