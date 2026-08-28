FROM golang:1-trixie AS builder

WORKDIR /app

ARG APP_VERSION=dev

ENV GO111MODULE=on
ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
	go build -ldflags="-X github.com/MaimoryLab/codeoff-server/internal/buildinfo.Version=${APP_VERSION}" -o codeoff-cli ./cmd/codeoff-cli
RUN --mount=type=cache,target=/root/.cache/go-build \
	go build -ldflags="-X github.com/MaimoryLab/codeoff-server/internal/buildinfo.Version=${APP_VERSION}" -o codeoff-daemon ./cmd/codeoff-daemon


FROM node:26-trixie

RUN sed -i 's/deb.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources && \
	sed -i 's/security.debian.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apt/sources.list.d/debian.sources
RUN mkdir -p --mode=0755 /usr/share/keyrings && \
	curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null && \
	echo 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main' | tee /etc/apt/sources.list.d/cloudflared.list && \
	apt-get update && apt-get install -y cloudflared sudo curl gnupg2

RUN groupadd -r user && useradd -r -g user -d /home/user user
RUN echo "user ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/user \
	&& chmod 0440 /etc/sudoers.d/user
USER user

WORKDIR /home/user

RUN sudo npm config set registry https://registry.npmmirror.com

RUN --mount=type=cache,target=/root/.npm \
	sudo npm install -g --cache /root/.npm @openai/codex

COPY --from=builder /app/codeoff-cli /usr/local/bin/codeoff-cli
COPY --from=builder /app/codeoff-daemon /usr/local/bin/codeoff-daemon

EXPOSE 11037

ENTRYPOINT ["codeoff-daemon"]
CMD ["codeoff-daemon", "-cf-tunnel", "-listen", "127.0.0.1:11037"]
