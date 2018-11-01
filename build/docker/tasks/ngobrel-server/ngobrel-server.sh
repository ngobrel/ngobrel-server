#!/bin/sh

if [ -f /conf/settings.sh ];then
    . /conf/settings.sh
fi


echo "Deploying DB...."
cd /migrate
./migrate.sh

echo "Running server..."

/bin/ngobrel-server