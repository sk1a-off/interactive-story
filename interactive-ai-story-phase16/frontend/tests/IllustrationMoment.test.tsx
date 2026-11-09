import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'
import { IllustrationMoment } from '../src/app/IllustrationMoment'

afterEach(cleanup)

test.each([1, 2, 3, 4])('shows %s images without selection controls', count => {
  const images = Array.from({ length: count }, (_, i) => ({ id: `image-${i}`, variant: i + 1, url: `/image-${i}.png` }))
  const view = render(<IllustrationMoment index={0} images={{ id: 'generation', sceneId: 'scene', momentIndex: 1, status: 'done', imageCount: count, width: 512, height: 768, images }} />)
  expect(screen.getAllByRole('img')).toHaveLength(count)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  expect(view.container.querySelector('.portrait')).toBeInTheDocument()
  expect(Boolean(view.container.querySelector('.single'))).toBe(count === 1)
})

test('shows the pinned number of placeholders at the requested aspect ratio', () => {
  render(<IllustrationMoment index={0} images={{ id: 'generation', sceneId: 'scene', momentIndex: 1, status: 'running', imageCount: 4, width: 768, height: 512, images: [] }} />)
  expect(screen.getAllByText(/Генерируется вариант/)).toHaveLength(4)
  expect(screen.getByText('Генерируется вариант 4…')).toHaveStyle({ aspectRatio: '768 / 512' })
})
