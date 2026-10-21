import type { CallDetails, CallStatus, CreatedCall, InviteeRole, JoinCredentials } from '../../domain/types.ts'

export interface CallGateway {
  createCall(input: {
    inviteeUserId: string
    inviteeRole: InviteeRole
    idempotencyKey: string
  }): Promise<CreatedCall>
  getCall(callId: string): Promise<CallDetails>
  endCall(callId: string): Promise<{ status: CallStatus }>
  issueJoinToken(callId: string): Promise<JoinCredentials>
}
