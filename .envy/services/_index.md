---
description: Auto-generated service reference from compose.yml.
menu:
    main:
        name: Services
        weight: 5
sidebar:
    hide: true
title: Services
weight: 5
---

{{< cards cols="2" >}}
{{< card title="cache" titleLink="/services/cache" cardType="service" dockerImage="valkey/valkey:7.2.5-bookworm" dockerImageLink="https://hub.docker.com/r/valkey/valkey" command="[ \"valkey-server\", \"--loglevel\", \"warning\" ]" >}}
{{< card title="db" titleLink="/services/db" cardType="service" description=`Describes db service configuration.` dockerImage="postgres:17.4-bookworm" dockerImageLink="https://hub.docker.com/_/postgres" tagsSets="db" tagsProfiles="dev,staging" >}}
{{< card title="proxy" titleLink="/services/proxy" cardType="service" description=`Describes proxy service configuration.` dockerImage="caddy:2.10" dockerImageLink="https://hub.docker.com/_/caddy" tagsProfiles="dev,staging" >}}
{{< card title="search" titleLink="/services/search" cardType="service" description=`Describes search service configuration. For details see` descriptionLink="https://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/" dockerImage="opensearchproject/opensearch:2.18.0" dockerImageLink="https://hub.docker.com/r/opensearchproject/opensearch" tagsSets="search" >}}
{{< card title="web" titleLink="/services/web" cardType="service" description=`Describes web service configuration.` dockerImage="ghcr.io/front-matter/invenio-rdm-starter:latest" dockerImageLink="https://ghcr.io/front-matter/invenio-rdm-starter" platform="linux/amd64" tagsSets="base,web,authentication,mail,s3" >}}
{{< card title="worker" titleLink="/services/worker" cardType="service" dockerImage="ghcr.io/front-matter/invenio-rdm-starter:latest" dockerImageLink="https://ghcr.io/front-matter/invenio-rdm-starter" platform="linux/amd64" command="[ \"celery\", \"-A\", \"invenio_app.celery\", \"worker\", \"--beat\", \"--schedule=/tmp/celerybeat-schedule\", \"--events\", \"--loglevel=WARNING\" ]" tagsSets="base,cache,doi,mail,s3" >}}
{{< /cards >}}
