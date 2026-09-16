import Link from "next/link";
import { UnavailableAuthIntent } from "../components/unavailable-auth-intent";

export default function NotFound() {
  return <main className="shell service-state"><h1>未找到该资料</h1><p>资料可能已下架、移动或尚未发布。</p><UnavailableAuthIntent /><Link className="button button--primary" href="/search">搜索其他资料</Link><Link className="button button--quiet" href="/">返回首页</Link></main>;
}
