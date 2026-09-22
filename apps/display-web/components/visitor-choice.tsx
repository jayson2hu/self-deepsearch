"use client";

import Link from "next/link";
import {useState,useSyncExternalStore} from "react";
import {getVisitorChoice,setVisitorChoice,visitorChoiceEvent} from "../lib/visitor-metrics";

function subscribe(callback:()=>void){window.addEventListener("storage",callback);window.addEventListener(visitorChoiceEvent,callback);return()=>{window.removeEventListener("storage",callback);window.removeEventListener(visitorChoiceEvent,callback);};}

export function VisitorChoice({enabled,inline=false}:{enabled:boolean;inline?:boolean}){
  const choice=useSyncExternalStore(subscribe,getVisitorChoice,()=>"unset");
  const [error,setError]=useState("");
  if(!enabled)return inline?<p>本环境尚未启用访客 UV 采集；不生成访客标识。</p>:null;
  if(!inline&&choice!=="unset")return null;
  const choose=(value:"allow"|"deny")=>{setError(setVisitorChoice(value)?"":"浏览器存储不可用，未启用访客统计。")};
  return <section className={inline?"visitor-choice-inline":"visitor-choice"} aria-label="访客统计选择">
    <strong>访客统计由你选择</strong><p>允许后，用保留最长 30 天的本地随机标识统计详情独立访客，不使用 IP 或设备指纹。拒绝不影响浏览；普通浏览次数仍会累计。<Link href="/privacy#visitor-metrics">了解详情和修改选择</Link></p>
    {inline?<p>当前选择：{choice==="allow"?"允许（浏览器 GPC/DNT 开启时仍不发送）":choice==="deny"?"不允许":"尚未选择"}。从后续详情访问开始生效。</p>:null}
    <div><button type="button" onClick={()=>choose("allow")}>允许访客统计</button><button type="button" onClick={()=>choose("deny")}>不允许访客统计</button></div>
    {error?<p role="status">{error}</p>:null}
  </section>;
}
