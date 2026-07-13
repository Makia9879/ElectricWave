# Nginx Deployment Templates

These files mirror the VPS Nginx Compose layout. They are deployed in stages:

1. Deploy `templates/notice-http.conf` as `conf/conf.d/notice.example.com.conf`.
2. Obtain the Let's Encrypt certificate with the shared `certbot/www` webroot.
3. Replace the active site with `templates/notice-https.conf`.
4. When the notification service exists, add the documented webhook, SSE, and test route proxies to the HTTPS server.

The `certbot/` directory is remote-only state and must never be committed.
