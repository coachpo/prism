import { createContext, useContext } from "react";

const passthrough = <T,>(read: Promise<T>): Promise<T> => read;
export const ObserveReadCycleContext = createContext({ track: passthrough, isBusy: (): boolean => false, revision: 0, busy: false as boolean, advance: () => {} });


export function useObserveReadCycle() { return useContext(ObserveReadCycleContext); }
