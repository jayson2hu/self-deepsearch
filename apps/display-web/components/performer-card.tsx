import type { HomeItem } from "@self-deepsearch/api-contracts";
import Link from "next/link";
import { SafeImage } from "./safe-image";

export function PerformerCard({ item }: { item: HomeItem }) {
  const image = item.image;
  return <article className="performer-card">
    <Link className="performer-card__visual" href={item.href} aria-label={`查看 ${item.title} 的人物资料`}>
      <SafeImage
        src={image?.url ?? item.image_url ?? "/default-performer.svg"}
        renditions={image?.renditions}
        fallbackSrc="/default-performer.svg"
        sizes="(max-width: 760px) calc(50vw - 17px), (max-width: 1050px) calc(25vw - 22px), 260px"
        alt=""
        width={480}
        height={600}
      />
    </Link>
    <div><h2><Link href={item.href}>{item.title}</Link></h2><p>{item.subtitle}</p></div>
  </article>;
}
