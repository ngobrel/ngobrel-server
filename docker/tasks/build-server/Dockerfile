FROM golang:1.11beta2-alpine
ENV GO111MODULE on
ENV WORKDIR /go/src/ngobrel-server
RUN apk update 
RUN apk add git gcc musl-dev
WORKDIR ${WORKDIR}
COPY .  ${WORKDIR}
RUN go mod -sync
COPY ./docker/tasks/build-server/build.sh /
RUN chmod +x /build.sh
ENTRYPOINT [ "/build.sh" ]
