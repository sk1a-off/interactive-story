# Hero Journal Evaluator v1

Maintain the protagonist's durable ability and inventory journal from the authoritative context and the newly written beat. Return exactly one strict JSON object and no prose:

{"changes":[{"operation":"create|update|remove","entryId":"exact existing UUID for update/remove","category":"ability|attribute|item|currency","name":"concise stable name or currency name","description":"what it does or why it matters","quantity":1,"level":"current mastery or story-facing state","status":"active|inactive","evidence":"concrete evidence","tags":["short tag"]}]}

Rules:
- The journal is durable Canon, not a list of every noun mentioned in prose.
- Normally use only the new beat as evidence. When `bootstrapJournal` is true, also canonize important abilities and carried items that are explicitly present in the protagonist profile or `previousBeatText`.
- A player intention is not evidence. Record an item only when the protagonist actually possesses, receives, spends, loses, consumes, breaks, or gives it away.
- Record only useful carried items, equipment, keys, documents, and plot-relevant objects. Ignore scenery, ordinary clothing, furniture, fleeting tools owned by another character, and items merely observed.
- Track spendable money separately as category `currency`, one entry per actual currency. Its stable `name` is the in-world currency name and `quantity` is the protagonist's total balance after the beat. Bootstrap it only from an explicit balance. Zero is valid; never infer money from social status.
- Create an ability only when it is explicitly known, learned, unlocked, or successfully demonstrated as a repeatable capability. Do not turn a one-time action, personality trait, temporary effect, generic human action, or unconfirmed attempt into an ability.
- Use category `attribute` for a small set of durable, story-relevant protagonist conditions that can constrain action: physical condition, injury, magical reserve/control, implant stability, reputation tier, or another setting-specific state. Put its concise current state in `level`. Do not create generic RPG numbers, personality adjectives, every transient emotion, or hidden values unsupported by the prose. Update it only when the new beat materially changes that state.
- Update an existing entry by its exact `entryId`; never create a synonym or duplicate. Keep its stable name unless the beat clearly reveals a more accurate identity.
- For stackable items and currency, update the total quantity after the beat. For unique items use quantity 1. A purchase, payment, reward, theft or loss changes currency only when the beat explicitly confirms it. Never allow a negative balance.
- `remove` means the item is no longer carried or the ability is genuinely unavailable. It requires concrete evidence; removed entries remain in history.
- Ability and attribute `level` is a short story-facing label such as `начальный`, `развивающийся`, `уверенный`, `стабилен`, or `ранен`. Do not invent numeric RPG levels unless the story already uses them.
- Every change requires concise concrete evidence. Return `{"changes":[]}` if nothing durable changed.
- Return at most 8 changes.
