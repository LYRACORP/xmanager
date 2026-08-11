# XManager Adminer — Adminer 6 + mongo/redis/elastic/clickhouse drivers
FROM php:8.3-apache-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
      libssl-dev pkg-config curl unzip \
    && pecl install mongodb \
    && docker-php-ext-enable mongodb \
    && a2enmod rewrite \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /var/www/html

# Adminer 6.0.0 English single-file
ADD https://github.com/vrana/adminer/releases/download/v6.0.0/adminer-6.0.0-en.php /var/www/html/index.php

# Plugin drivers (Adminer 6 auto-loads adminer-plugins/*.php)
RUN mkdir -p /var/www/html/adminer-plugins
ADD https://raw.githubusercontent.com/vrana/adminer/refs/tags/v6.0.0/plugins/drivers/mongo.php \
    /var/www/html/adminer-plugins/mongo.php
ADD https://raw.githubusercontent.com/vrana/adminer/refs/tags/v6.0.0/plugins/drivers/redis.php \
    /var/www/html/adminer-plugins/redis.php
ADD https://raw.githubusercontent.com/vrana/adminer/refs/tags/v6.0.0/plugins/drivers/elastic.php \
    /var/www/html/adminer-plugins/elastic.php
ADD https://raw.githubusercontent.com/vrana/adminer/refs/tags/v6.0.0/plugins/drivers/clickhouse.php \
    /var/www/html/adminer-plugins/clickhouse.php

# Prefer system packages; disable version check noise
RUN printf '%s\n' '<?php' \
    'namespace Adminer;' \
    'class AdminerVersionNoverify {' \
    '  function head() { echo "<script>verifyVersion=()=>{};</script>"; }' \
    '}' \
    > /var/www/html/adminer-plugins/version-noverify.php

RUN chown -R www-data:www-data /var/www/html \
    && chmod -R a+rX /var/www/html

EXPOSE 80
