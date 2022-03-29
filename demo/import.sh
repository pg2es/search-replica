#!/usr/bin/env bash

# fastest way of importing data :)
(
echo 'TRUNCATE TABLE "main_doc" ,"child_doc" ,"inline_doc";'
echo 'COPY "main_doc" FROM STDIN WITH CSV;'
cat ./data/main_doc.csv
echo '\.'

echo 'COPY "child_doc" FROM STDIN WITH CSV;'
cat ./data/child_doc.csv
echo '\.'

echo 'COPY "inline_doc" FROM STDIN WITH CSV;'
cat ./data/inline_doc.csv
echo '\.'
) | psql -h 127.0.0.1 -U postgres -d postgres -f -


