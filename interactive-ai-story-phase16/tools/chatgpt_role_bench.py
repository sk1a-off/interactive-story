#!/usr/bin/env python3
"""Run frozen role tests through an already running experimental browser bridge.
No production settings/database writes. Requires binaries and an active-prompt snapshot.
"""
import argparse
import concurrent.futures
import datetime
import json
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
BASE = 'http://127.0.0.1:8791'
GROUPS = {
 'setup': 'setup_story_bible,setup_initial_cast,setup_quest_stages,setup_prose_second,action_interpreter',
 'writer': 'director,writer_continuation,writer_large_context,browser_escaping',
 'evaluators': 'state_evaluator,quest_evaluator,choices,image_prompt,turn_planner_browser,world_browser_positive,world_browser_negative',
}

def main():
 parser=argparse.ArgumentParser(description=__doc__)
 parser.add_argument('--output',required=True,type=Path)
 parser.add_argument('--cases',default='',help='comma-separated subset; one isolated process per case')
 parser.add_argument('--min-workers',type=int,default=3,choices=[1,2,3])
 args=parser.parse_args()
 groups=GROUPS
 if args.cases:
  names=[n.strip() for n in args.cases.split(',') if n.strip()]
  allowed={n for values in GROUPS.values() for n in values.split(',')}
  if not names or len(names)!=len(set(names)) or not set(names)<=allowed:parser.error('unknown or duplicate case')
  groups={name:name for name in names}
 expected=sum(len(values.split(',')) for values in groups.values())
 out=args.output.resolve();out.mkdir(parents=True,exist_ok=False)
 html=urllib.request.urlopen(BASE,timeout=5).read().decode()
 token=re.search(r'name="bridge-token" content="([a-f0-9]{48})"',html).group(1)
 keyfile=ROOT/'data/run/chatgpt-benchmark-key.json'
 fd=os.open(keyfile,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
 with os.fdopen(fd,'w') as f:json.dump({'bridge':token},f)
 def status():
  req=urllib.request.Request(BASE+'/status',headers={'Authorization':'Bearer '+token})
  return json.load(urllib.request.urlopen(req,timeout=5))
 deadline=time.monotonic()+120
 while True:
  before=status();now=datetime.datetime.now(datetime.timezone.utc)
  ready=sum(w['status']=='ready' and (now-datetime.datetime.fromisoformat(w['seen'].replace('Z','+00:00'))).total_seconds()<15 for w in before['workers'].values())
  if ready>=args.min_workers:break
  if time.monotonic()>deadline:raise RuntimeError('Not enough fresh ready workers; no test jobs submitted')
  time.sleep(2)
 (out/'bridge-before.json').write_text(json.dumps(status(),ensure_ascii=False,indent=2)+'\n')
 def run(item):
  group,cases=item
  cmd=[str(ROOT/'data/run/chatgpt-role-bench'),'-browser-bridge-bench','-browser-prompt-snapshot',str(ROOT/'data/run/chatgpt-active-prompts.json'),'-endpoint',BASE+'/v1/chat/completions','-model','gpt-6-instant','-skip-warmup','-secret-store',str(keyfile),'-secret-name','bridge','-case',cases,'-output',str(out/(group+'.json'))]
  with (out/(group+'.log')).open('w') as log:
   p=subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT,timeout=1800)
  print(group,'exit=',p.returncode,flush=True)
  return group,p.returncode
 with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
  outcomes=dict(pool.map(run,groups.items()))
 final=status();(out/'bridge-after.json').write_text(json.dumps(final,ensure_ascii=False,indent=2)+'\n')
 rows=[]
 for group in groups:
  path=out/(group+'.json')
  if path.exists():rows.extend(json.loads(path.read_text())['cases'])
 summary={'status':'complete' if all(v==0 for v in outcomes.values()) and len(rows)==expected else 'interrupted','expected_cases':expected,'finished':datetime.datetime.now(datetime.timezone.utc).isoformat(),'processes':outcomes,'count':len(rows),'transport_ok':sum(r['ok'] for r in rows),'cases':[{'name':r['name'],'ok':r['ok'],'ms':r['elapsedMs'],'validation':r['validation'],'prose':r.get('proseMetrics'),'error':r.get('error')} for r in rows], 'notes':['Frozen synthetic cases using active prompt snapshot; production settings and Canon unchanged.','Browser temperature, token limits and native JSON schema are not enforced; token usage/TPS unavailable.','Model messages are serialized into one physical line for the contenteditable transport.','Role fixtures run independently; this is not a committed full story turn or a comparison with current Google routing.']}
 (out/'summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps({'count':summary['count'],'transport_ok':summary['transport_ok'],'directory':str(out)}),flush=True)

if __name__=='__main__':main()
