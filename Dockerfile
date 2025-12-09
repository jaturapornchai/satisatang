# Build stage
FROM golang:1.24-alpine AS builder

# Install git for fetching dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source code into the image
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o satisatang main.go

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the pre-built binary file from the previous stage
COPY --from=builder /app/satisatang .

# Expose port 3000 to the outside world
EXPOSE 3000

# Command to run the executable
CMD ["./satisatang"]
