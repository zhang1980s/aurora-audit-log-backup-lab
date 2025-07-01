FROM public.ecr.aws/lambda/provided:al2023-arm64

# Install necessary tools
RUN dnf install -y tar gzip git

# Set Go version
ENV GOVERSION=1.24.4
ENV GOARCH=arm64
ENV GOOS=linux

# Download and install Go
RUN curl -sL https://go.dev/dl/go${GOVERSION}.${GOOS}-${GOARCH}.tar.gz -o go.tar.gz && \
    tar -C /usr/local -xzf go.tar.gz && \
    rm go.tar.gz

# Set Go environment variables
ENV PATH=$PATH:/usr/local/go/bin
ENV GOPATH=/go
ENV PATH=$PATH:$GOPATH/bin

# Create app directory
WORKDIR /app

# Copy Go module files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Build the application
RUN go build -o bootstrap main.go

# Set the CMD to the handler
CMD [ "bootstrap" ]