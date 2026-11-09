import { describe, expect, test } from 'vitest'
import { illustrationLayout, paragraphCanBeIllustrated } from '../src/lib/illustrationPlacement'

describe('paragraphCanBeIllustrated', () => {
 test('offers illustration only for substantial visual prose', () => {
  expect(paragraphCanBeIllustrated('Через минуту они продолжили путь.')).toBe(false)
  expect(paragraphCanBeIllustrated('— За перевалом уже горят огни, и если мы поспешим, то успеем пройти через ворота до полуночи, — сказал проводник.')).toBe(false)
  expect(paragraphCanBeIllustrated('Над затопленной площадью медленно поднялся огромный механический маяк, и холодный синий свет отразился в окнах, воде и лицах замерших у колоннады людей.')).toBe(true)
 })
})

test('illustration layout preserves supported aspect ratios and legacy defaults', () => {
 expect(illustrationLayout({})).toEqual({aspectRatio:'768 / 768',orientation:'square',count:2})
 expect(illustrationLayout({width:512,height:768,imageCount:1})).toEqual({aspectRatio:'512 / 768',orientation:'portrait',count:1})
 expect(illustrationLayout({width:768,height:512,imageCount:4})).toEqual({aspectRatio:'768 / 512',orientation:'landscape',count:4})
 expect(illustrationLayout({width:0,height:0,imageCount:99})).toEqual({aspectRatio:'768 / 768',orientation:'square',count:2})
})
