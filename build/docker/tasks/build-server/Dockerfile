FROM golang:1.11beta2-alpine
ENV GO111MODULE on
ENV WORKDIR /go/src/ngobrel-server
RUN apk update 
RUN apk add git gcc musl-dev
WORKDIR ${WORKDIR}
COPY ./pkg/ngobrel/ ${WORKDIR}/
RUN mkdir -p ${WORKDIR}/cmd/ngobrel-server
COPY ./cmd/ngobrel-server/* ${WORKDIR}/cmd/ngobrel-server/
RUN find ${WORKDIR}/
RUN go mod -v -sync
COPY ./build/docker/tasks/build-server/build.sh /
RUN chmod +x /build.sh
ENTRYPOINT [ "/build.sh" ]
