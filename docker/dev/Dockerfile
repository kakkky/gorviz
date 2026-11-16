# 開発用。ソースは compose でマウントし、air が変更のたびに templ generate と build をして gorviz-launcher を起動し直す
FROM golang:1.27

ARG AIR_VERSION=latest
ARG TEMPL_VERSION=v0.3.1070
RUN go install github.com/air-verse/air@${AIR_VERSION} \
	&& go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION}

WORKDIR /app
CMD ["air", "-c", ".air.toml"]
