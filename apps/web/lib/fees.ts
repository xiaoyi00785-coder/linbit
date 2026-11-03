/** linbit 交易手续费（无 VIP 阶梯，所有用户统一） */
export const SPOT_FEE = { maker: 0.001, taker: 0.001 } as const; // 现货 0.1%，按成交额收取，从所得币种中扣除
export const FUTURES_FEE = { maker: 0.0005, taker: 0.0005 } as const; // 合约 0.05%，开仓与平仓各收一次
/** 合约单边手续费 = 仓位价值 × 0.05% */
export const futuresFee = (positionValue: number) => positionValue * FUTURES_FEE.taker;
export const pct = (r: number) => `${+(r * 100).toFixed(3)}%`;
