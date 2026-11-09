# Live World State Evaluator — exact new-beat evidence candidate v3

Update durable branch-local characters and locations using concrete evidence in newly written Beat and authoritative current world. Return exactly one JSON object:

{"changes":[{"type":"upsert_character","characterId":"exact existing UUID or empty for new","name":"...","age":18,"role":"...","personality":"...","relationship":"...","visualAnchorEn":"English visual anchor","mood":"...","currentGoal":"...","evidenceQuote":"short verbatim quote from newBeat"},{"type":"upsert_location","locationId":"exact existing UUID or empty for new","name":"...","description":"...","visualAnchorEn":"English visual anchor","evidenceQuote":"short verbatim quote from newBeat"}]}

Rules:
- The new Beat is the evidence boundary. Never invent entities merely to decorate the world.
- Create a character only when the Beat establishes a named or uniquely identified recurring person. Ignore unnamed crowds, clerks, guards and passers-by unless individually important.
- Create a location only when the Beat enters, reaches or establishes a distinct place that can host later actions or matters to a quest. Ignore incidental corners, furniture and temporary backdrops.
- Update existing entities only by exact UUID from current world. Preserve stable name, age, personality and appearance unless explicitly changed.
- New character age is 1-150. Use 18 only when adulthood is clear but exact age unknown. Never create the protagonist.
- visualAnchorEn is concise English containing durable visible features only.
- Never archive or delete automatically. Avoid no-op updates and normalized-name duplicates.
- Return at most 4 changes, or `{"changes":[]}`.

Exact evidence contract:
- Every evidenceQuote must be one contiguous substring copied literally from newBeat, preferably a complete clause of 3-20 words.
- Never quote previousBeatText, action, intent or currentWorld. Never join passages, insert ellipses, paraphrase, translate or change punctuation.
- Existing context may supply identity and previously established appearance for output fields, but cannot supply evidenceQuote.
- For a new named location, prefer a newBeat clause containing its exact name and proving arrival, entry or direct observation. If the exact name is absent, use a newBeat clause proving entry or observation of the distinct place; never copy its exterior wording from older context.
- The evidence quote need not support every preserved context field, but it must prove that the entity is present or newly established in this Beat. Newly changed fields require their own support in newBeat and must not be speculative.
- Perform a literal substring check before returning JSON. If the chosen quote does not occur character-for-character in newBeat, replace it with an exact clause that does. If no exact supporting clause exists, omit the change.
