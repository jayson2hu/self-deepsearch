const choiceKey="mujian_analytics_choice_v1";
const visitorKey="mujian_visitor_v1";
const lifetime=30*24*60*60*1000;
export const visitorChoiceEvent="mujian-analytics-choice";
export type VisitorChoice="allow"|"deny"|"unset";

export function getVisitorChoice():VisitorChoice{
  try{const choice=localStorage.getItem(choiceKey);return choice==="allow"||choice==="deny"?choice:"unset";}catch{return "unset";}
}

export function setVisitorChoice(choice:"allow"|"deny"):boolean{
  try{localStorage.setItem(choiceKey,choice);if(choice==="deny")localStorage.removeItem(visitorKey);window.dispatchEvent(new Event(visitorChoiceEvent));return true;}catch{return false;}
}

export function getVisitorToken():string|undefined{
  if(typeof window==="undefined"||document.querySelector('meta[name="mujian-visitor-metrics"]')?.getAttribute("content")!=="enabled")return;
  if(getVisitorChoice()!=="allow"||(navigator as Navigator&{globalPrivacyControl?:boolean}).globalPrivacyControl||navigator.doNotTrack==="1")return;
  try{
    const now=Date.now(),saved=JSON.parse(localStorage.getItem(visitorKey)??"null") as {token?:string;expiresAt?:number}|null;
    if(saved&&typeof saved.token==="string"&&/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(saved.token)&&typeof saved.expiresAt==="number"&&saved.expiresAt>now&&saved.expiresAt<=now+lifetime)return saved.token;
    // getRandomValues also works on the existing HTTP test origin; no fingerprint.
    const bytes=crypto.getRandomValues(new Uint8Array(16));bytes[6]=(bytes[6]&15)|64;bytes[8]=(bytes[8]&63)|128;
    const hex=Array.from(bytes,b=>b.toString(16).padStart(2,"0")).join("");
    const token=`${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
    localStorage.setItem(visitorKey,JSON.stringify({token,expiresAt:now+lifetime}));return token;
  }catch{return;}
}
