import api from './client'
import { parseCapabilitiesDocument } from './capabilitiesContract'

export const capabilitiesAPI = {
  get: () =>
    api
      .get<unknown>('/v1/capabilities')
      .then((response) => parseCapabilitiesDocument(response.data)),
}
