import type { SceneImageGeneration } from '../lib/api'
import { illustrationLayout } from '../lib/illustrationPlacement'
import '../styles/illustrations.css'

export function IllustrationMoment({ images, index }: { images: SceneImageGeneration; index: number }) {
  const layout = illustrationLayout(images)
  const single = images.status === 'done' ? images.images.length === 1 : layout.count === 1
  return <section className={`illustration-moment inline-illustration ${layout.orientation}`} data-generation-id={images.id}>
    <header><span className="eyebrow">Иллюстрация {index + 1}</span></header>
    <div className={`illustration-variants${single ? ' single' : ''}`}>
      {(images.status === 'pending' || images.status === 'running') && Array.from({ length: layout.count }, (_, position) =>
        <div className="scene-image-skeleton" style={{ aspectRatio: layout.aspectRatio }} key={position}>Генерируется вариант {position + 1}…</div>)}
      {images.status === 'done' && images.images.map((image, position) => <figure className="scene-image-card" key={image.id}>
        <img src={image.url} alt={`Иллюстрация ${index + 1}, изображение ${position + 1}`} />
      </figure>)}
      {images.status === 'failed' && <div className="scene-image-failed"><p>Не удалось сгенерировать эту иллюстрацию.</p></div>}
    </div>
  </section>
}
