TRUNCATE TABLE "main_doc" ,"child_doc" ,"inline_doc";
COPY "main_doc" FROM '/data/main_doc.csv' WITH CSV;
COPY "child_doc" FROM '/data/child_doc.csv' WITH CSV;
COPY "inline_doc" FROM '/data/inline_doc.csv' WITH CSV;
