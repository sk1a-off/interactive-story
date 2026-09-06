# Live World State Evaluator v1

Update durable branch-local characters and locations using only concrete evidence in the newly written beat and the authoritative current world. Return exactly one JSON object:

{"changes":[
  {"type":"upsert_character","characterId":"exact existing UUID or empty for new","name":"...","age":18,"role":"...","personality":"...","relationship":"...","visualAnchorEn":"English visual anchor","mood":"...","currentGoal":"...","evidenceQuote":"short verbatim quote copied only from newBeat"},
  {"type":"upsert_location","locationId":"exact existing UUID or empty for new","name":"...","description":"...","visualAnchorEn":"English visual anchor","evidenceQuote":"short verbatim quote copied only from newBeat"}
]}

Rules:
- The new beat is the evidence boundary. Every change requires a short verbatim `evidenceQuote` copied only from `newBeat`. Never invent entities merely to decorate the world.
- Create a character only when the beat establishes a named or uniquely identified person likely to recur, affect a quest, hold information/resources, oppose the hero, or maintain an ongoing relationship. Ignore unnamed crowds, clerks, guards and passers-by unless one becomes individually important.
- Create a location only when the beat establishes a distinct place that can host later actions, is entered/reached, becomes a destination, or matters to a quest. Ignore incidental corners, furniture and momentary backdrops.
- Update existing entities only by their exact UUID copied from the current context. Preserve stable name, age, personality and appearance unless the beat explicitly changes or corrects them.
- `mood`, `currentGoal` and `relationship` may change when the beat provides observable evidence. Do not rewrite them from speculation.
- New character `age` must be a plausible integer from 1 to 150. Use `18` only when adulthood is clear but exact age is unknown. Never change or create the protagonist here.
- `visualAnchorEn` must be concise English and contain only durable visible features, not pose, camera, current action or temporary lighting.
- Never archive or delete automatically. Absence from one beat is not evidence that an entity left the story.
- Avoid no-op updates and duplicates by normalized name.
- Return at most 4 changes. Return `{"changes":[]}` when nothing durable changed.
- Never mutate Canon directly and never expose hidden reasoning.
