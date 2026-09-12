import {act,render,screen,waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach,describe,it,expect,vi} from "vitest";
import {RouteExplanationPanel} from "./RouteExplanationPanel";
import {routeExplanation} from "@/lib/api";
import {TooltipProvider} from "@/components/ui/tooltip";
import {LocaleProvider} from "@/i18n/LocaleProvider";
const auth = vi.hoisted(() => ({epoch:1,listeners:new Set<() => void>()}));
vi.mock("@/context/auth/coordinatorInstance", () => ({authSessionCoordinator:{getEpoch:()=>auth.epoch,subscribe:(listener:()=>void)=>{auth.listeners.add(listener);return ()=>auth.listeners.delete(listener);}}}));
vi.mock("@/lib/api",()=>({routeExplanation:{get:vi.fn()}}));
vi.mock("@/hooks/useTimezone",()=>({useTimezone:()=>({formatWithZone:(x:string)=>x})}));
vi.mock("@tanstack/react-router",()=>({Link:({children,to}: {children:React.ReactNode;to:string})=><a href={to}>{children}</a>}));
const sample={generation:"7",published_at:"2026-09-10T10:00:00Z",observed_at:"2026-09-10T10:01:00Z",sample_completed_at:"2026-09-10T10:01:01Z",model_config_id:1,operation:"openai.chat_completions",completeness:"partial" as const,boundary:"sampled",exclusions:[],candidates:[{path:["entry","child"],strategy:"round-robin",model_config_id:2,model_id:"child",terminal_target_id:3,planner_position:1,schedule:"open",runtime_observed:false,reason:"planner_candidate",capacity:"not_observed"}]};
describe("route explanation",()=>{
 beforeEach(()=>{vi.mocked(routeExplanation.get).mockReset();auth.epoch=1;});
 it("only samples on action and preserves stale facts after failure",async()=>{
  vi.mocked(routeExplanation.get).mockResolvedValueOnce(sample).mockRejectedValueOnce(new Error("offline"));
  render(<LocaleProvider><TooltipProvider><RouteExplanationPanel modelId={1} apiFamily="openai"/></TooltipProvider></LocaleProvider>);
  expect(routeExplanation.get).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button",{name:"检查连接"}));
  expect(await screen.findByText(/部分连接状态缺失/)).toBeInTheDocument();
  expect(screen.getByText(/尚无请求记录/)).toBeInTheDocument();
  expect(screen.getByRole("link",{name:/entry → child/})).toHaveAttribute("href","/route/models/2");
  await userEvent.click(screen.getByRole("button",{name:"检查连接"}));
  expect(await screen.findByText(/陈旧：/)).toBeInTheDocument();
  expect(screen.getByText(/候选 1/)).toBeInTheDocument();
 });
 it("cancels a pending observation when leaving",async()=>{
  vi.mocked(routeExplanation.get).mockImplementation(()=>new Promise(()=>{}));
  const view=render(<LocaleProvider><TooltipProvider><RouteExplanationPanel modelId={1} apiFamily="openai"/></TooltipProvider></LocaleProvider>);
  await userEvent.click(screen.getByRole("button",{name:"检查连接"}));
  await waitFor(()=>expect(routeExplanation.get).toHaveBeenCalledTimes(1));
  const signal=vi.mocked(routeExplanation.get).mock.calls[0][2];
  view.unmount();expect(signal?.aborted).toBe(true);
 });

 it("clears completed evidence and resets operation on same-model family changes",async()=>{
  vi.mocked(routeExplanation.get).mockResolvedValue(sample);
  const panel=(family:string)=><LocaleProvider><TooltipProvider><RouteExplanationPanel modelId={1} apiFamily={family}/></TooltipProvider></LocaleProvider>;
  const view=render(panel("openai"));
  await userEvent.click(screen.getByRole("button",{name:"检查连接"}));
  await screen.findByText(/部分连接状态缺失/);
  view.rerender(panel("anthropic"));
  expect(screen.queryByText(/部分连接状态缺失/)).not.toBeInTheDocument();
  expect(screen.getByRole("combobox")).toHaveTextContent("Anthropic 消息");
  await userEvent.click(screen.getByRole("button",{name:"检查连接"}));
  expect(vi.mocked(routeExplanation.get).mock.lastCall?.[1]).toBe("anthropic.messages");
 });
 it("clears completed evidence and cancels pending observations on auth epoch changes",async()=>{
  vi.mocked(routeExplanation.get).mockResolvedValueOnce(sample).mockImplementationOnce(()=>new Promise(()=>{}));
  render(<LocaleProvider><TooltipProvider><RouteExplanationPanel modelId={1} apiFamily="openai"/></TooltipProvider></LocaleProvider>);
  await userEvent.click(screen.getByRole("button",{name:"检查连接"}));
  await screen.findByText(/部分连接状态缺失/);
  await userEvent.click(screen.getByRole("button",{name:"检查连接"}));
  const signal=vi.mocked(routeExplanation.get).mock.lastCall?.[2];
  act(()=>{auth.epoch++;auth.listeners.forEach(listener=>listener());});
  expect(signal?.aborted).toBe(true);
  expect(screen.queryByText(/部分连接状态缺失/)).not.toBeInTheDocument();
  expect(screen.getByRole("button",{name:"检查连接"})).toBeEnabled();
 });
});
