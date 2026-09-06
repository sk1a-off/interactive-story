# Canon Extractor v1 — benchmark only

Extract only durable changes explicitly established by newBeat. Return one strict JSON object:

{"worldChanges":[{"type":"upsert_character|upsert_location","characterId":"existing UUID","locationId":"existing UUID","name":"...","age":18,"role":"...","personality":"...","relationship":"...","visualAnchorEn":"English durable appearance","mood":"...","currentGoal":"...","description":"...","evidenceQuote":"verbatim newBeat quote"}],"journalChanges":[{"operation":"create|update|remove","entryId":"existing UUID","category":"ability|attribute|item|currency","name":"...","description":"...","quantity":1,"level":"...","status":"active|inactive","evidenceQuote":"verbatim newBeat quote","tags":[]}],"objectiveChanges":[{"operation":"create|progress|complete|fail","objectiveId":"existing UUID","reference":"...","parentObjectiveId":"...","parentReference":"...","scope":"global|minor","kind":"quest|task|event|milestone","questType":"main|side","title":"...","description":"...","successCriteria":"observable condition","status":"active|completed|failed","progress":0,"evidenceQuote":"verbatim newBeat quote"}]}

Rules:
- newBeat is the only evidence boundary. Action, previous Beat and older context are not evidence.
- Every mutation requires a short verbatim evidenceQuote copied from newBeat.
- Emit no-op arrays when no durable state changed; do not turn every noun or action into Canon.
- Update existing records only by exact UUID. Never create synonyms or duplicate entities, journal entries or quests.
- Intent is not success. Complete an objective only when the quote directly demonstrates its observable success criteria.
- A new quest or stage is active and unresolved; never create historical completed work.
- Item/currency quantities change only when newBeat explicitly confirms possession, spending, loss or gain.
- Do not invent abilities, attributes, NPCs or locations from implications.
- At most 4 worldChanges, 8 journalChanges and 6 objectiveChanges. No prose outside JSON.
