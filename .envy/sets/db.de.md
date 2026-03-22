---
description: DB defines database configuration.
hideTitle: true
title: db
toc: false
weight: 4
---

{{< cards cols="1" >}}
{{< card title="db" cardType="set" description=`DB defines database configuration.` tagsServices="db" >}}
{{< /cards >}}

<div id="postgres_host"></div>

{{< cards cols="1" >}}
{{< card link="" title="POSTGRES_HOST" cardType="var" var="postgres" >}}
{{< /cards >}}

<div id="postgres_db"></div>

{{< cards cols="1" >}}
{{< card link="" title="POSTGRES_DB" cardType="var" var="inveniordm" >}}
{{< /cards >}}

<div id="postgres_user"></div>

{{< cards cols="1" >}}
{{< card link="" title="POSTGRES_USER" cardType="var" var="postgres" >}}
{{< /cards >}}

<div id="postgres_password"></div>

{{< cards cols="1" >}}
{{< card link="" title="POSTGRES_PASSWORD" cardType="var" tagBottom="Geheim" tagBottomColor="orange" >}}
{{< /cards >}}

<div id="invenio_sqlalchemy_database_uri"></div>

{{< cards cols="1" >}}
{{< card link="" title="INVENIO_SQLALCHEMY_DATABASE_URI" cardType="var" var="envy:readonly:postgresql+psycopg2://${POSTGRES_USER:-inveniordm}:${POSTGRES_PASSWORD:-}@${POSTGRES_HOST:-postgres}:5432/${POSTGRES_DB:-inveniordm}" >}}
{{< /cards >}}

