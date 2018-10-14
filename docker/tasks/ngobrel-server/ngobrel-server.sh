#!/bin/sh

if [ -f /conf/settings.sh ];then
    . /conf/settings.sh
fi


echo "Deploying DB...."
cd /deploy
./deploy.sh

echo "Running server..."

/bin/ngobrel-server