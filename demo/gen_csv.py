#!/usr/bin/env python3
from sys import argv
from os import path
from random import randint
import csv
import json
from datetime import datetime
from faker import Faker

fake = Faker()

dirr = "./data/" 
mainf = open(path.join(dirr, 'main_doc.csv'), 'w')
childf = open(path.join(dirr, 'child_doc.csv'), 'w')
inlinef = open(path.join(dirr, 'inline_doc.csv'), 'w')

child_csv = csv.DictWriter(childf, fieldnames=['id','parent_id','value','ignore_me'])
main_csv = csv.DictWriter(mainf, fieldnames=['id','date','deleted','nested','non_searchable_field','text','text_array','ignore_me'])
inline_csv = csv.DictWriter(inlinef, fieldnames=['id','parent_id','value','ignore_me'])

def childs(i):
    for j in range(randint(5, 25)):
        yield {
            'id': f'CHILD{i:06d}:{j:02d}',
            'parent_id': f'ID{i:06d}',
            'value': fake.text(max_nb_chars=30),
            'ignore_me': fake.text(max_nb_chars=30, ext_word_list=['child', 'doc', 'dbonly', 'noforward']),
        }
def inlines(i):
    for j in range(randint(0, 3)):
        yield {
            'id': f'INLINE{i:06d}:{j:02d}',
            'parent_id': f'ID{i:06d}',
            'value': fake.text(max_nb_chars=30),
            'ignore_me': fake.text(max_nb_chars=30, ext_word_list=['child', 'doc', 'dbonly', 'noforward']),
        }


def main(i):
    return {
        'id': f'ID{i:06d}',
        'date': fake.date_time_this_decade().isoformat(sep=' '),
        'deleted': randint(1,100) == 100, # 1% chance of being deleted
        'nested': json.dumps({
            "key": fake.nic_handle(suffix='KEY'),
            "value": '{:.2f}'.format(fake.pyfloat(left_digits=2, right_digits=2,
                positive=True, min_value=5, max_value=99)),
            "name": fake.company(),
        }),
        'non_searchable_field': fake.text(max_nb_chars=100) ,
        'text': fake.text(max_nb_chars=50),
        'text_array': "{'" +  "','".join(fake.sentences(nb=3)) + "'}",
        'ignore_me': fake.text(max_nb_chars=50, ext_word_list=['Alert', 'DBonly', 'noforward', 'youshouldnotseeme']),
    }

argv.append(1000) # default number of documents to denerate
for i in range(int(argv[1])):
    main_csv.writerow(main(i))
    for child in childs(i):
        child_csv.writerow(child)
    for inline in inlines(i):
        inline_csv.writerow(inline)


mainf.close()
inlinef.close()
childf.close()









