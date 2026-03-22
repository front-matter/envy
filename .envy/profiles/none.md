---
hideTitle: true
title: none
toc: false
weight: 1
---

{{< cards cols="2" >}}
{{< card title="cache" link="/services/cache" cardType="service" dockerImage="valkey/valkey:7.2.5-bookworm" dockerImageLink="https://hub.docker.com/r/valkey/valkey" command="[ \"valkey-server\", \"--loglevel\", \"warning\" ]" >}}
{{< card title="search" link="/services/search" cardType="service" description=`Describes search service configuration. For details see
https://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/` dockerImage="opensearchproject/opensearch:2.18.0" dockerImageLink="https://hub.docker.com/r/opensearchproject/opensearch" tagsSets="search" >}}
{{< card title="web" link="/services/web" cardType="service" description=`Describes web service configuration.` dockerImage="ghcr.io/front-matter/invenio-rdm-starter:latest" dockerImageLink="https://ghcr.io/front-matter/invenio-rdm-starter" platform="linux/amd64" tagsSets="base,web,authentication,mail,s3" >}}
{{< card title="worker" link="/services/worker" cardType="service" dockerImage="ghcr.io/front-matter/invenio-rdm-starter:latest" dockerImageLink="https://ghcr.io/front-matter/invenio-rdm-starter" platform="linux/amd64" command="[ \"celery\", \"-A\", \"invenio_app.celery\", \"worker\", \"--beat\", \"--schedule=/tmp/celerybeat-schedule\", \"--events\", \"--loglevel=WARNING\" ]" tagsSets="base,cache,doi,mail,s3" >}}
{{< /cards >}}
