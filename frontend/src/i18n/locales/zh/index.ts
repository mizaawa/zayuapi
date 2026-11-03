import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import batchImage from './batchImage'
import imageWorkbench from './imageWorkbench'
import videoWorkbench from './videoWorkbench'
import degradation from './degradation'
import admin from './admin'
import misc from './misc'

export default {
  ...landing,
  ...common,
  ...dashboard,
  ...batchImage,
  ...imageWorkbench,
  ...videoWorkbench,
  ...degradation,
  admin,
  ...misc,
}
