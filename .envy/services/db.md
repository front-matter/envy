---
description: Describes db service configuration.
hideTitle: true
title: db
toc: false
weight: 2
---

{{< cards cols="1" >}}
{{< card title="db" cardType="service" description=`Describes db service configuration.` dockerImage="postgres:17.4-bookworm" dockerImageLink="https://hub.docker.com/_/postgres" tagsSets="db" tagsProfiles="dev,staging" >}}
{{< /cards >}}

<div id="invenio_sqlalchemy_database_uri"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_SQLALCHEMY_DATABASE_URI" cardType="var" var="envy:readonly:postgresql+psycopg2://${POSTGRES_USER:-inveniordm}:${POSTGRES_PASSWORD:-}@${POSTGRES_HOST:-postgres}:5432/${POSTGRES_DB:-inveniordm}" >}}
{{< /cards >}}

<div id="postgres_db"></div>

{{< cards cols="1" >}}
{{< card link="" title="POSTGRES_DB" cardType="var" var="inveniordm" >}}
{{< /cards >}}

<div id="postgres_host"></div>

{{< cards cols="1" >}}
{{< card link="" title="POSTGRES_HOST" cardType="var" var="postgres" >}}
{{< /cards >}}

<div id="postgres_password"></div>

{{< cards cols="1" >}}
{{< card link="" title="POSTGRES_PASSWORD" cardType="var" >}}
{{< /cards >}}

<div id="postgres_user"></div>

{{< cards cols="1" >}}
{{< card link="" title="POSTGRES_USER" cardType="var" var="postgres" >}}
{{< /cards >}}

