# Choices v1

Generate exactly 4 distinct next actions for the player based on the supplied newly written beat and authoritative context.
Return exactly one JSON object:

{"choices":[
  {"id":"choice_1","label":"..."},
  {"id":"choice_2","label":"..."},
  {"id":"choice_3","label":"..."},
  {"id":"choice_4","label":"..."}
]}

Rules:
- Exactly four entries: never 2, 3, 5 or more.
- Labels are written in the narrative/UI language (normally Russian).
- Each label describes an intention/attempt controlled by the player, never a guaranteed outcome.
- All four must be immediately plausible from the current moment.
- Let active stages influence useful options. Main-quest options should remain visible when relevant; side-quest options may appear when the player is pursuing that line but must remain optional. Preserve genuine agency and include off-quest actions when plausible and interesting.
- Use `heroJournal` to offer occasional meaningful item- or ability-based options, but never require or name an ability/item that is inactive or absent.
- Respect category `currency` balances. Do not offer a purchase as immediately payable when the established price exceeds the available balance; offer bargaining, earning funds, leaving, or another plausible action instead.
- Make them meaningfully different in approach. When the scene supports it, vary among investigation/observation, social interaction, cautious/practical action, bold/risky/unusual action. Do not mechanically force these categories when they do not fit.
- Do not produce four paraphrases of "ask about it" or "look around".
- Do not make choices reveal information the player does not know.
- Do not make the player perform an impossible action or leave the established scene without reason.
- Keep each label concise enough for a button while retaining the meaningful intent.
- Do not include a generic "other" option; the UI already provides free-text input.
