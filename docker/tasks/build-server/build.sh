#!/bin/sh

cd ${WORKDIR}/cmd/ngobrel-server
go build -v -x -ldflags "-extldflags '-static'" -o /srv/ngobrel-server