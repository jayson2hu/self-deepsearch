import type { HomeItem } from "@self-deepsearch/api-contracts";
import { EyeOff } from "lucide-react";
import Link from "next/link";
import { SafeImage } from "./safe-image";

export function WorkCard({ item, position, onHide }: { item: HomeItem; position: number; onHide?: (id: string) => void }) {
  const image = item.image;
  return (
    <article className="work-card">
      <Link className="work-card__visual" href={item.href} aria-label={`查看 ${item.code ?? item.title}`}>
        <span className="work-card__rank">{String(position).padStart(2, "0")}</span>
        <SafeImage
          src={image?.url ?? item.image_url ?? "/default-work.svg"}
          renditions={image?.renditions}
          fallbackSrc="/default-work.svg"
          sizes="(max-width: 760px) calc(50vw - 17px), (max-width: 1050px) calc(25vw - 22px), 260px"
          alt=""
          width={640}
          height={400}
        />
      </Link>
      <div className="work-card__body">
        {item.code && <p className="work-card__code">{item.code}</p>}
        <h3><Link href={item.href}>{item.title}</Link></h3>
        <p>{item.subtitle}</p>
        {onHide ? <button className="card-action" type="button" onClick={() => onHide(item.id)}><EyeOff size={14} />不感兴趣</button> : null}
      </div>
    </article>
  );
}
