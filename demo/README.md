### Getting started:
To run local setup docker-compose demo use following command:
```bash
# build & start everything (DB schema, index mapping, data)
docker-compose up -d --build

# stop and remove data
docker-compose down -v
```

Have fun!
E.G: Modify data in PGADMIN `demo@demo.com:demo`, exposed on port `:5000` and see how it get's updated in elastic

You can also generate any amount of data with  `gen_csv.py` and import it using `import.sh`


TRY THIS QUERY
```json
{
    "query": {
        "bool": {
            "should": [
                {
                    "has_child": {
                        "type": "immachild",
                        "inner_hits": {
                            "_source": true
                        },
                        "query": {
                            "match_all": {}
                        },
                        "min_children": 1
                    }
                }
            ]
        }
    }
}
```
```bash
curl --location --request GET 'http://127.0.0.1:9200/postgres/_search?filter_path=**.hits._source&pretty=true' --header 'Content-Type: application/json' --data-raw '{"query":{"bool": {"should": [ { "has_child": { "type": "immachild", "inner_hits": { "_source": true }, "query": { "match_all": {} }, "min_children": 1 } } ] } } }'
```
