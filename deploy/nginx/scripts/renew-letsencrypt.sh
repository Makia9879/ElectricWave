#!/bin/sh
set -eu

base_dir=/home/mk/docker-compose/nginx

docker run --rm \
  -v "$base_dir/certbot/conf:/etc/letsencrypt" \
  -v "$base_dir/certbot/www:/var/www/certbot" \
  certbot/certbot:latest \
  renew \
  --webroot \
  -w /var/www/certbot \
  --quiet \
  --deploy-hook 'docker exec nginx nginx -s reload'
