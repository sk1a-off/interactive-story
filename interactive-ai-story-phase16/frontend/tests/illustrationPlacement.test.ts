import { describe, expect, test } from 'vitest'
import { paragraphCanBeIllustrated } from '../src/lib/illustrationPlacement'

describe('paragraphCanBeIllustrated', () => {
 test('offers illustration only for substantial visual prose', () => {
  expect(paragraphCanBeIllustrated('Через минуту они продолжили путь.')).toBe(false)
  expect(paragraphCanBeIllustrated('— За перевалом уже горят огни, и если мы поспешим, то успеем пройти через ворота до полуночи, — сказал проводник.')).toBe(false)
  expect(paragraphCanBeIllustrated('Над затопленной площадью медленно поднялся огромный механический маяк, и холодный синий свет отразился в окнах, воде и лицах замерших у колоннады людей.')).toBe(true)
 })
})
