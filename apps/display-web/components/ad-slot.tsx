type AdSlotProps = {
  label: string;
  placement: "banner" | "rail" | "infeed" | "footer";
};

export function AdSlot({ label, placement }: AdSlotProps) {
  return (
    <aside
      className={`ad-slot ad-slot--${placement}`}
      aria-label={`广告占位：${label}`}
      data-ad-placement={placement}
      data-ad-state="placeholder"
    >
      <span className="ad-slot__mark">广告</span>
      <strong>{label}</strong>
      <small>测试占位，不加载第三方脚本</small>
    </aside>
  );
}
